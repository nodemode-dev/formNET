package server

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nodemode-dev/formnet/internal/app"
	"github.com/nodemode-dev/formnet/internal/flow"
	"github.com/nodemode-dev/formnet/internal/rules"
	"github.com/nodemode-dev/formnet/internal/userfile"
)

const (
	sessionsDir = "sessions"
	// maxSessionFile bounds an uploaded .formnet file. Gzip shrinks text a
	// lot but images and other compressed bodies hardly at all, so a full
	// recording at the default 256 MiB body budget can come near 350 MiB.
	maxSessionFile = 512 << 20
	// maxSessionJSON bounds what a session file may decompress to, so a
	// small file that inflates without end stops here; flows past the
	// Store's limits are dropped while reading, which keeps memory near the
	// Store's budget, not near this bound.
	maxSessionJSON = 1 << 30
	// maxHARFile bounds an imported HAR, which carries its bodies inline as
	// text or base64 and is not compressed.
	maxHARFile = 512 << 20
)

var reservedNames = []string{"CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9"}

// SessionStatus is /api/status's session: whether the list shows a session
// file that was opened. Live capture goes on into the same list while it is
// open; the opened flows carry from_session.
type SessionStatus struct {
	Open       bool       `json:"open"`
	Name       string     `json:"name"`
	Saved      *time.Time `json:"saved"`
	Flows      int        `json:"flows"`
	Dropped    int        `json:"dropped"`
	AppVersion string     `json:"app_version,omitempty"`
	Opened     *time.Time `json:"opened"`
}

type dataState struct {
	mu      sync.Mutex
	session SessionStatus
}

// sessionMessages is a proxy that can hand out and take WebSocket messages for session files.
type sessionMessages interface {
	SessionMessages(flowID string) []flow.SessionMessage
	LoadMessages(flowID string, msgs []flow.SessionMessage, sum *flow.MessageSummary)
}

func (s *handler) sessionStatus() SessionStatus {
	if s.data == nil {
		return SessionStatus{}
	}
	s.data.mu.Lock()
	defer s.data.mu.Unlock()
	return s.data.session
}

func (s *handler) sessionClosed() {
	if s.data == nil {
		return
	}
	s.data.mu.Lock()
	s.data.session = SessionStatus{}
	s.data.mu.Unlock()
}

// registerData adds sessions, HAR import and the cURL export.
func (s *handler) registerData(mux *http.ServeMux) {
	s.data = &dataState{}
	mux.HandleFunc("POST /api/session/save", s.saveSession)
	mux.HandleFunc("GET /api/session/export", s.exportSession)
	mux.HandleFunc("GET /api/sessions", s.listSessions)
	mux.HandleFunc("POST /api/session/open", s.openSession)
	mux.HandleFunc("POST /api/import/har", s.importHAR)
	mux.HandleFunc("GET /api/export/curl", s.exportCurl)
}

// search applies the property filters and the body search to a list query.
func (s *handler) search(r *http.Request, q flow.Query) ([]flow.Flow, flow.SearchStats, error) {
	fl, err := filterFrom(r.URL.Query())
	if err != nil {
		return nil, flow.SearchStats{}, err
	}
	for _, x := range []struct {
		name string
		in   *string
	}{{"host", &q.Host}, {"domain", &q.Domain}} {
		rest, not := strings.CutPrefix(*x.in, "!")
		if !not {
			continue
		}
		if rest = strings.TrimSpace(rest); rest == "" {
			return nil, flow.SearchStats{}, fmt.Errorf("%s: give a value after !", x.name)
		}
		if fl == nil {
			fl = flow.NewFilter()
		}
		*x.in = ""
		if x.name == "host" {
			fl.NotHost = rest
		} else {
			fl.NotDomain = rest
		}
	}
	flows, st := s.Store.Search(q, fl)
	return flows, st, nil
}

// negated reads a filter value whose leading ! inverts it; a bare ! is an error naming the field.
func negated(v url.Values, name string) (string, bool, error) {
	raw := strings.TrimSpace(v.Get(name))
	rest, not := strings.CutPrefix(raw, "!")
	if not && strings.TrimSpace(rest) == "" {
		return "", false, fmt.Errorf("%s: give a value after !", name)
	}
	return strings.TrimSpace(rest), not, nil
}

// hostGlobs compiles a comma separated list of host globs with the rules matcher.
func hostGlobs(field, list string) (func(string) bool, error) {
	var hosts []*rules.Matcher
	for i, h := range strings.Split(list, ",") {
		if h = strings.TrimSpace(h); h == "" {
			continue
		}
		m, err := rules.Compile(rules.Match{Host: h}, field+"["+strconv.Itoa(i)+"]")
		if err != nil {
			return nil, err
		}
		hosts = append(hosts, m)
	}
	if len(hosts) == 0 {
		return nil, nil
	}
	return func(host string) bool {
		return slices.ContainsFunc(hosts, func(m *rules.Matcher) bool { return m.MatchHost(host) })
	}, nil
}

func optionalBool(v url.Values, name string) (*bool, error) {
	raw := v.Get(name)
	if raw == "" {
		return nil, nil
	}
	b, err := strconv.ParseBool(raw)
	if err != nil {
		return nil, fmt.Errorf("%s must be 1 or 0", name)
	}
	return &b, nil
}

// filterFrom reads the property filters and the body search; nil when none is given.
func filterFrom(v url.Values) (*flow.Filter, error) {
	names := []string{"status", "method", "type", "min_size", "max_size", "min_ms", "max_ms", "error", "rules", "throttled", "from_session", "process", "focus", "ignore", "body"}
	if !slices.ContainsFunc(names, func(n string) bool { return v.Get(n) != "" }) {
		return nil, nil
	}
	fl := flow.NewFilter()
	st, not, err := negated(v, "status")
	if err != nil {
		return nil, err
	}
	if st = strings.ToLower(st); st != "" {
		lo, hi, err := statusRange(st)
		if err != nil {
			return nil, err
		}
		fl.StatusMin, fl.StatusMax, fl.StatusNot = lo, hi, not
	}
	methods, not, err := negated(v, "method")
	if err != nil {
		return nil, err
	}
	for _, m := range strings.Split(methods, ",") {
		if m = strings.ToUpper(strings.TrimSpace(m)); m != "" {
			fl.Methods = append(fl.Methods, m)
		}
	}
	fl.MethodsNot = not
	if fl.TypePrefix, fl.TypeNot, err = negated(v, "type"); err != nil {
		return nil, err
	}
	fl.TypePrefix = strings.ToLower(fl.TypePrefix)
	for _, n := range []struct {
		name string
		dst  *int64
	}{{"min_size", &fl.MinSize}, {"max_size", &fl.MaxSize}} {
		if raw := v.Get(n.name); raw != "" {
			x, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || x < 0 {
				return nil, fmt.Errorf("%s must be a number of bytes, 0 or more", n.name)
			}
			*n.dst = x
		}
	}
	for _, n := range []struct {
		name string
		dst  *float64
	}{{"min_ms", &fl.MinMS}, {"max_ms", &fl.MaxMS}} {
		if raw := v.Get(n.name); raw != "" {
			x, err := strconv.ParseFloat(raw, 64)
			if err != nil || x < 0 {
				return nil, fmt.Errorf("%s must be a number of milliseconds, 0 or more", n.name)
			}
			*n.dst = x
		}
	}
	for _, b := range []struct {
		name string
		dst  **bool
	}{{"error", &fl.Error}, {"rules", &fl.Rules}, {"throttled", &fl.Throttled}, {"from_session", &fl.FromSession}} {
		if *b.dst, err = optionalBool(v, b.name); err != nil {
			return nil, err
		}
	}
	if fl.Process, fl.ProcessNot, err = negated(v, "process"); err != nil {
		return nil, err
	}
	fl.Process = strings.ToLower(fl.Process)
	if fl.Focus, err = hostGlobs("focus", v.Get("focus")); err != nil {
		return nil, err
	}
	if fl.Ignore, err = hostGlobs("ignore", v.Get("ignore")); err != nil {
		return nil, err
	}
	body := v.Get("body")
	if rest, ok := strings.CutPrefix(body, "!"); ok {
		if rest == "" {
			return nil, errors.New("body: give a value after !")
		}
		body, fl.BodyNot = rest, true
	}
	fl.Body = body
	return fl, nil
}

// statusRange reads 404, 4xx or 200-299.
func statusRange(st string) (int, int, error) {
	bad := errors.New("status must be a code such as 404, a class such as 4xx, or a range such as 200-299")
	code := func(s string) (int, bool) {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		return n, err == nil && n >= 100 && n <= 999
	}
	if len(st) == 3 && st[1:] == "xx" && st[0] >= '1' && st[0] <= '9' {
		lo := int(st[0]-'0') * 100
		return lo, lo + 99, nil
	}
	if a, b, ok := strings.Cut(st, "-"); ok {
		lo, ok1 := code(a)
		hi, ok2 := code(b)
		if !ok1 || !ok2 || lo > hi {
			return 0, 0, bad
		}
		return lo, hi, nil
	}
	n, ok := code(st)
	if !ok {
		return 0, 0, bad
	}
	return n, n, nil
}

func (s *handler) dataFolder(w http.ResponseWriter) (string, bool) {
	if s.Data == "" {
		writeError(w, http.StatusServiceUnavailable, "this run has no data folder, so sessions cannot be saved or opened here")
		return "", false
	}
	dir := filepath.Join(s.Data, sessionsDir)
	fi, err := os.Lstat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			writeError(w, http.StatusInternalServerError, "the sessions folder could not be made: "+err.Error())
			return "", false
		}
		fi, err = os.Lstat(dir)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return "", false
	}
	if fi.Mode()&fs.ModeSymlink != 0 || !fi.IsDir() {
		writeError(w, http.StatusConflict, dir+" is not a plain folder; sessions are kept only in a real folder there")
		return "", false
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(dir, 0o700); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return "", false
		}
	}
	return dir, true
}

// sessionName checks a session's name: a plain name, the .formnet ending optional.
func sessionName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if strings.HasSuffix(strings.ToLower(name), flow.SessionExt) {
		name = name[:len(name)-len(flow.SessionExt)]
	}
	if name == "" || len(name) > 100 || strings.ContainsAny(name, `/\:*?"<>|`) || strings.Contains(name, "..") || filepath.Base(name) != name || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return "", errors.New("name must be a plain name such as checkout-bug: letters, digits, spaces, dots, dashes, underscores; no folders, separators or ..")
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return "", errors.New("name must not hold control characters")
		}
	}
	stem := strings.ToUpper(strings.TrimSpace(strings.SplitN(name, ".", 2)[0]))
	if slices.Contains(reservedNames, stem) {
		return "", fmt.Errorf("%s is a reserved device name on Windows; choose another name", stem)
	}
	return name, nil
}

func (s *handler) messagesFor() func(string) []flow.SessionMessage {
	if sm, ok := s.Protocol.(sessionMessages); ok {
		return sm.SessionMessages
	}
	return nil
}

func (s *handler) saveSession(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name *string `json:"name"`
	}
	if !decodeStrict(w, r, 4<<10, &in, `body must be JSON: {"name": "checkout-bug"}`) {
		return
	}
	if in.Name == nil {
		writeError(w, http.StatusBadRequest, `name: is required, a plain name such as checkout-bug`)
		return
	}
	name, err := sessionName(*in.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	dir, ok := s.dataFolder(w)
	if !ok {
		return
	}
	target := filepath.Join(dir, name+flow.SessionExt)
	if filepath.Dir(target) != dir {
		writeError(w, http.StatusBadRequest, "name must stay inside the sessions folder")
		return
	}
	f, err := userfile.Create(target)
	if errors.Is(err, fs.ErrExist) {
		writeError(w, http.StatusConflict, name+flow.SessionExt+" already exists; a save never replaces a file, so choose another name")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	saved := time.Now().UTC()
	bw := bufio.NewWriterSize(f, 256<<10)
	zw := gzip.NewWriter(bw)
	n, err := flow.WriteSession(zw, s.Store, s.Store.List(flow.Query{}), app.Version, saved, s.messagesFor())
	if cerr := zw.Close(); err == nil {
		err = cerr
	}
	if ferr := bw.Flush(); err == nil {
		err = ferr
	}
	if ferr := f.Close(); err == nil {
		err = ferr
	}
	if err != nil {
		_ = os.Remove(target)
		writeError(w, http.StatusInternalServerError, "the session was not saved: "+err.Error())
		return
	}
	size := int64(0)
	if fi, err := os.Stat(target); err == nil {
		size = fi.Size()
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": name, "path": target, "flows": n, "bytes": size, "saved": saved})
}

func (s *handler) exportSession(w http.ResponseWriter, r *http.Request) {
	q, err := queryFrom(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	listed, _, err := s.search(r, q)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	saved := time.Now().UTC()
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="formnet-`+saved.Format("20060102-150405")+flow.SessionExt+`"`)
	zw := gzip.NewWriter(w)
	if _, err := flow.WriteSession(zw, s.Store, listed, app.Version, saved, s.messagesFor()); err != nil {
		s.Log.Warn("session export", "err", err)
	}
	_ = zw.Close()
}

type savedSession struct {
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	Saved      time.Time `json:"saved"`
	Flows      int       `json:"flows"`
	Bytes      int64     `json:"bytes"`
	AppVersion string    `json:"app_version"`
	Error      string    `json:"error,omitempty"`
}

func (s *handler) listSessions(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.dataFolder(w)
	if !ok {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := []savedSession{}
	for _, e := range entries {
		if !e.Type().IsRegular() || !strings.HasSuffix(e.Name(), flow.SessionExt) || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		full := filepath.Join(dir, e.Name())
		item := savedSession{Name: strings.TrimSuffix(e.Name(), flow.SessionExt), Path: full}
		if fi, err := e.Info(); err == nil {
			item.Bytes = fi.Size()
			item.Saved = fi.ModTime().UTC()
		}
		if f, err := os.Open(full); err == nil {
			head, err := flow.ReadSessionHead(f)
			_ = f.Close()
			if err != nil || head.Format != flow.SessionFormat {
				item.Error = "not a readable FormNET session file"
			} else {
				item.Flows, item.AppVersion = head.FlowCount, head.AppVersion
				if !head.Saved.IsZero() {
					item.Saved = head.Saved
				}
			}
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Saved.After(out[j].Saved) })
	writeJSON(w, http.StatusOK, map[string]any{"folder": dir, "sessions": out})
}

func (s *handler) limits(maxBytes int64) flow.SessionLimits {
	n, budget := s.Store.Limits()
	return flow.SessionLimits{MaxBytes: maxBytes, Flows: n, BodyBytes: budget}
}

// upload returns the file a request carries: the "file" part of a
// multipart form, or the body itself; name is the file's name without its ending.
func upload(r *http.Request, ext string) (io.Reader, string, error) {
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mt != "multipart/form-data" {
		return r.Body, r.URL.Query().Get("name"), nil
	}
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, "", errors.New("the multipart form could not be read: " + err.Error())
	}
	for {
		part, err := mr.NextPart()
		if err != nil {
			return nil, "", errors.New(`the form has no "file" part`)
		}
		if part.FormName() == "file" || part.FileName() != "" {
			name := filepath.Base(part.FileName())
			if strings.HasSuffix(strings.ToLower(name), ext) {
				name = name[:len(name)-len(ext)]
			}
			return part, name, nil
		}
	}
}

func readFailure(w http.ResponseWriter, err error, what string) {
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &tooBig):
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("%s may be at most %d MiB", what, tooBig.Limit>>20))
	case errors.Is(err, flow.ErrSessionTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("%s decompresses to more than %d MiB, more than FormNET reads", what, maxSessionJSON>>20))
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

func (s *handler) openSession(w http.ResponseWriter, r *http.Request) {
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	r.Body = http.MaxBytesReader(w, r.Body, maxSessionFile)
	var src io.Reader
	var name string
	var named *string
	if mt != "multipart/form-data" {
		br := bufio.NewReaderSize(r.Body, 64<<10)
		head, _ := br.Peek(4<<10 + 1)
		var in struct {
			Name   *string `json:"name"`
			Format *string `json:"format"`
		}
		if len(head) <= 4<<10 && json.Unmarshal(head, &in) == nil && in.Format == nil {
			if in.Name == nil {
				writeError(w, http.StatusBadRequest, `name: is required, the name of a saved session (GET /api/sessions lists them), or send a .formnet file`)
				return
			}
			named = in.Name
		}
		src, name = br, r.URL.Query().Get("name")
	}
	if named != nil {
		var err error
		if name, err = sessionName(*named); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		dir, ok := s.dataFolder(w)
		if !ok {
			return
		}
		full := filepath.Join(dir, name+flow.SessionExt)
		fi, err := os.Lstat(full)
		if err != nil || !fi.Mode().IsRegular() {
			writeError(w, http.StatusNotFound, "no saved session named "+name)
			return
		}
		f, err := os.Open(full)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer f.Close()
		src = f
	} else {
		if mt == "multipart/form-data" {
			var err error
			if src, name, err = upload(r, flow.SessionExt); err != nil {
				readFailure(w, err, "a session file")
				return
			}
		}
		if clean, err := sessionName(name); err == nil {
			name = clean
		} else {
			name = "uploaded session"
		}
	}
	sess, err := flow.ReadSession(src, s.limits(maxSessionJSON))
	if err != nil {
		readFailure(w, err, "a session file")
		return
	}
	s.Store.Replace(sess.Flows)
	messages := s.loadMessages(sess.Flows, sess.Messages)
	now := time.Now().UTC()
	st := SessionStatus{Open: true, Name: name, Flows: len(sess.Flows), Dropped: sess.Dropped, AppVersion: sess.AppVersion, Opened: &now}
	if !sess.Saved.IsZero() {
		saved := sess.Saved
		st.Saved = &saved
	}
	s.data.mu.Lock()
	s.data.session = st
	s.data.mu.Unlock()
	s.Log.Info("session opened", "name", name, "flows", len(sess.Flows), "dropped", sess.Dropped)
	writeJSON(w, http.StatusOK, map[string]any{"session": st, "flows": len(sess.Flows), "dropped": sess.Dropped, "messages": messages})
}

// loadMessages hands opened WebSocket messages to the proxy and returns how many it took.
func (s *handler) loadMessages(flows []flow.Flow, msgs map[string][]flow.SessionMessage) int {
	sm, ok := s.Protocol.(sessionMessages)
	if !ok {
		return 0
	}
	n := 0
	for _, f := range flows {
		if list, ok := msgs[f.ID]; ok && f.Kind == flow.KindWebSocket {
			if _, still := s.Store.Get(f.ID); still {
				sm.LoadMessages(f.ID, list, f.Messages)
				n += len(list)
			}
		}
	}
	return n
}

func (s *handler) importHAR(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxHARFile)
	src, _, err := upload(r, ".har")
	if err != nil {
		readFailure(w, err, "a HAR file")
		return
	}
	limit := s.BodyLimit
	if limit <= 0 {
		limit = 256 << 10
	}
	imp, err := flow.ImportHAR(src, limit, s.limits(maxHARFile))
	if err != nil {
		readFailure(w, err, "a HAR file")
		return
	}
	s.Store.Append(imp.Flows)
	messages := s.loadMessages(imp.Flows, imp.Messages)
	s.Log.Info("HAR imported", "flows", len(imp.Flows), "skipped", imp.Skipped, "creator", imp.Creator)
	writeJSON(w, http.StatusOK, map[string]any{"imported": len(imp.Flows), "skipped": imp.Skipped, "dropped": imp.Dropped, "messages": messages, "creator": imp.Creator})
}
