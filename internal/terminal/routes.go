package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/nodemode-dev/formnet/internal/agentlookup"
)

var titles = map[string]string{"claude": "Claude Code", "codex": "Codex", "cursor": "Cursor", "antigravity": "Antigravity"}

// Kinds are the tabs this engine opens.
var Kinds = []string{"shell", "claude", "codex", "cursor", "antigravity"}

// Register adds the dock's routes. The server's guard has already checked the
// Host, the run's token (bearer header, or ?token= on the stream) and, for
// writes, that the request comes from the page.
func (m *Manager) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/terminal", m.handleList)
	mux.HandleFunc("POST /api/terminal", m.handleOpen)
	mux.HandleFunc("GET /api/terminal/agents", m.handleAgents)
	mux.HandleFunc("POST /api/terminal/{id}/resize", m.handleResize)
	mux.HandleFunc("POST /api/terminal/{id}/restart", m.handleRestart)
	mux.HandleFunc("DELETE /api/terminal/{id}", m.handleClose)
	mux.HandleFunc("GET /api/terminal/{id}/stream", m.handleStream)
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func fail(w http.ResponseWriter, status int, code, msg string) {
	reply(w, status, map[string]string{"error": msg, "code": code})
}

func (m *Manager) handleList(w http.ResponseWriter, r *http.Request) {
	ok, why := supported()
	reply(w, http.StatusOK, map[string]any{"contract": Contract, "terminals": m.list(), "kinds": Kinds, "available": ok, "reason": why, "live": m.live(), "max": maxLive})
}

func (m *Manager) handleAgents(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"contract": Contract}
	for kind, info := range m.status(r.URL.Query().Get("fresh") == "1") {
		out[kind] = info
	}
	reply(w, http.StatusOK, out)
}

type openBody struct {
	Kind    string `json:"kind"`
	Cols    int    `json:"cols"`
	Rows    int    `json:"rows"`
	Title   string `json:"title"`
	Cwd     string `json:"cwd"`
	Capture bool   `json:"capture"`
	Prompt  string `json:"prompt"`
	Resume  string `json:"resume"`
	Fresh   bool   `json:"fresh"`
}

func (m *Manager) handleOpen(w http.ResponseWriter, r *http.Request) {
	var in openBody
	raw, err := io.ReadAll(io.LimitReader(r.Body, 512<<10))
	if err == nil && strings.TrimSpace(string(raw)) != "" {
		err = json.Unmarshal(raw, &in)
	}
	if err != nil {
		fail(w, http.StatusBadRequest, "bad_request", "body must be JSON: {kind, cols, rows, title, cwd, capture, prompt, resume, fresh} (each optional)")
		return
	}
	if in.Kind == "" {
		in.Kind = "shell"
	}
	_, program := titles[in.Kind]
	switch {
	case in.Kind != "shell" && !program:
		fail(w, http.StatusBadRequest, "bad_request", "kind must be one of shell, claude, codex, cursor, antigravity")
		return
	case len([]rune(in.Title)) > 60, len([]rune(in.Prompt)) > 100000, len(in.Resume) > 200:
		fail(w, http.StatusBadRequest, "bad_request", "title is at most 60 characters, prompt at most 100000, resume at most 200")
		return
	case (in.Prompt != "" || in.Resume != "" || in.Fresh) && in.Kind != "claude":
		fail(w, http.StatusBadRequest, "bad_request", "prompt, resume and fresh are for Claude Code tabs only")
		return
	}
	if ok, why := supported(); !ok {
		fail(w, http.StatusServiceUnavailable, "unavailable", why)
		return
	}
	cwd := home()
	if in.Cwd != "" {
		if st, err := os.Stat(in.Cwd); err != nil || !st.IsDir() || !filepath.IsAbs(in.Cwd) {
			fail(w, http.StatusBadRequest, "bad_request", "cwd must be the full path of a folder on this computer")
			return
		}
		cwd = in.Cwd
	}
	cols, rows := clamp(in.Cols, 10, 500, 100), clamp(in.Rows, 4, 200, 30)
	if in.Kind == "claude" && in.Resume == "" && !in.Fresh {
		if s := m.runningClaude(in.Capture); s != nil {
			if in.Prompt != "" {
				s.write([]byte(in.Prompt + "\r"))
			}
			reply(w, http.StatusOK, s.snapshot())
			return
		}
	}
	s := &Session{kind: in.Kind, title: strings.TrimSpace(in.Title), cwd: cwd, capture: in.Capture, cols: cols, rows: rows}
	if program {
		found := m.program(in.Kind)
		if found.State != "runs" {
			reply(w, http.StatusNotFound, map[string]string{"error": said(found), "code": "not_installed", "install": agentlookup.Install(in.Kind, "")})
			return
		}
		if err := agentlookup.Guard(found.Path); err != nil {
			fail(w, http.StatusInternalServerError, "failed", err.Error())
			return
		}
		args := []string{found.Path}
		if in.Resume != "" {
			args = append(args, "--resume", in.Resume)
		} else if p := strings.TrimSpace(in.Prompt); p != "" {
			args = append(args, p)
		}
		s.env = m.childEnv(found.Path, in.Capture)
		s.argv = agentlookup.StartArgv(args, s.env, "")
		if s.title == "" {
			s.title = titles[in.Kind]
		}
	} else {
		s.env = m.childEnv("", in.Capture)
		s.argv = loginShell()
	}
	s.tip = agentlookup.Short(cwd, "")
	if in.Capture {
		s.tip += " · its traffic goes through FormNET"
		s.status = "Recording in Flows"
	}
	switch err := m.open(s); {
	case errors.Is(err, errBusy):
		fail(w, http.StatusTooManyRequests, "busy", err.Error())
	case errors.Is(err, errStopped):
		fail(w, http.StatusServiceUnavailable, "unavailable", err.Error())
	case err != nil:
		fail(w, http.StatusInternalServerError, "failed", "the terminal could not start: "+err.Error())
	default:
		m.o.Log.Info("terminal opened", "id", s.id, "kind", s.kind, "capture", s.capture)
		reply(w, http.StatusCreated, s.snapshot())
	}
}

func (m *Manager) runningClaude(capture bool) *Session {
	var newest *Record
	for _, r := range m.list() {
		if r.Kind == "claude" && r.Alive && r.Capture == capture {
			newest = &r
		}
	}
	if newest == nil {
		return nil
	}
	return m.get(newest.ID)
}

func (m *Manager) handleResize(w http.ResponseWriter, r *http.Request) {
	s := m.get(r.PathValue("id"))
	if s == nil {
		fail(w, http.StatusNotFound, "not_found", "no such terminal")
		return
	}
	var in struct {
		Cols int `json:"cols"`
		Rows int `json:"rows"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&in); err != nil {
		fail(w, http.StatusBadRequest, "bad_request", `body must be JSON: {"cols": n, "rows": n}`)
		return
	}
	s.resize(in.Cols, in.Rows)
	reply(w, http.StatusOK, map[string]any{"ok": true})
}

func (m *Manager) handleRestart(w http.ResponseWriter, r *http.Request) {
	s := m.get(r.PathValue("id"))
	if s == nil {
		fail(w, http.StatusNotFound, "not_found", "no such terminal")
		return
	}
	if !s.snapshot().Alive && m.live() >= maxLive {
		fail(w, http.StatusTooManyRequests, "busy", errBusy.Error())
		return
	}
	if err := s.restart(); err != nil {
		fail(w, http.StatusInternalServerError, "failed", "the terminal could not start again: "+err.Error())
		return
	}
	reply(w, http.StatusOK, s.snapshot())
}

func (m *Manager) handleClose(w http.ResponseWriter, r *http.Request) {
	m.remove(r.PathValue("id"))
	reply(w, http.StatusOK, map[string]any{"ok": true})
}

// sameOrigin admits the upgrade only from the page FormNET serves: a browser
// sends Origin on every WebSocket handshake, and a WebSocket is not covered by
// CORS, so without this any site the person visits could reach a shell.
func sameOrigin(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && u.Scheme == "http" && u.Host != "" && strings.EqualFold(u.Host, r.Host)
}

type control struct {
	Type string `json:"type"`
	Data string `json:"data"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

func (m *Manager) handleStream(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		fail(w, http.StatusForbidden, "forbidden", "the terminal answers the FormNET page only")
		return
	}
	s := m.get(r.PathValue("id"))
	if s == nil {
		fail(w, http.StatusNotFound, "not_found", "no such terminal")
		return
	}
	if m.sockets.Add(1) > maxSockets {
		m.sockets.Add(-1)
		fail(w, http.StatusTooManyRequests, "busy", "too many terminal connections are open")
		return
	}
	defer m.sockets.Add(-1)
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(1 << 20)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	x, backlog, alive := s.attach()
	defer s.detach(x)
	go func() {
		defer cancel()
		for {
			kind, raw, err := c.Read(ctx)
			if err != nil {
				return
			}
			var msg control
			if kind != websocket.MessageText || json.Unmarshal(raw, &msg) != nil {
				continue
			}
			switch msg.Type {
			case "input":
				s.write([]byte(msg.Data))
			case "resize":
				s.resize(msg.Cols, msg.Rows)
			}
		}
	}()
	send := func(kind websocket.MessageType, b []byte) error {
		wctx, done := context.WithTimeout(ctx, 10*time.Second)
		defer done()
		return c.Write(wctx, kind, b)
	}
	if send(websocket.MessageBinary, backlog) != nil {
		return
	}
	if alive {
	loop:
		for {
			select {
			case data, ok := <-x.ch:
				if !ok {
					break loop
				}
				if send(websocket.MessageBinary, data) != nil {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}
	closed, code := s.ended()
	if closed {
		_ = c.Close(websocket.StatusGoingAway, "the terminal was closed")
		return
	}
	exit, _ := json.Marshal(map[string]any{"type": "exit", "code": code})
	if send(websocket.MessageText, exit) == nil {
		_ = c.Close(websocket.StatusNormalClosure, "the program ended")
	}
}
