package rules

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/nodemode-dev/formnet/internal/userfile"
)

// FileName is the rules file inside the data folder.
const FileName = "rules.json"

// Version is the schema version this build writes.
const Version = 1

// MaxRules bounds the list.
const MaxRules = 500

// Action types.
const (
	ActionRewrite    = "rewrite"
	ActionMapRemote  = "map_remote"
	ActionMapLocal   = "map_local"
	ActionBlock      = "block"
	ActionBreakpoint = "breakpoint"
)

// Set is the file's content.
type Set struct {
	Version int    `json:"version"`
	Enabled bool   `json:"enabled"` // the global switch: false turns every rule off
	Rules   []Rule `json:"rules"`
}

// Rule is one entry of the ordered list.
type Rule struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Match   Match  `json:"match"`
	Action  Action `json:"action"`
}

// Action is what a rule does; Type picks which of the other fields count.
type Action struct {
	Type string `json:"type"`

	RequestHeaders  []HeaderOp `json:"request_headers,omitempty"`
	ResponseHeaders []HeaderOp `json:"response_headers,omitempty"`
	Host            string     `json:"host,omitempty"`
	Path            string     `json:"path,omitempty"`
	Query           []QueryOp  `json:"query,omitempty"`
	Status          int        `json:"status,omitempty"`
	Body            []BodyOp   `json:"body,omitempty"`

	To       string `json:"to,omitempty"`
	KeepHost bool   `json:"keep_host,omitempty"`

	LocalPath string `json:"local_path,omitempty"`

	Drop bool `json:"drop,omitempty"`

	Request  bool `json:"request,omitempty"`
	Response bool `json:"response,omitempty"`
}

// HeaderOp adds, sets or removes one header.
type HeaderOp struct {
	Op    string `json:"op"` // add, set, remove
	Name  string `json:"name"`
	Value string `json:"value,omitempty"`
}

// QueryOp sets or removes one query parameter.
type QueryOp struct {
	Op    string `json:"op"` // set, remove
	Name  string `json:"name"`
	Value string `json:"value,omitempty"`
}

// BodyOp finds and replaces in a text body.
type BodyOp struct {
	Target  string `json:"target,omitempty"` // request or response (the default)
	Find    string `json:"find"`
	Replace string `json:"replace"`
	Regex   bool   `json:"regex,omitempty"`
}

// Compiled is a validated rule ready to apply.
type Compiled struct {
	Rule
	Matcher *Matcher
	To      *url.URL
	finds   []*regexp.Regexp
}

// Engine holds the rules of one data folder.
type Engine struct {
	path string
	data string
	mu   sync.Mutex
	cur  atomic.Pointer[state]
}

type state struct {
	set      Set
	compiled []*Compiled
}

// Open reads <dir>/rules.json. A missing file gives an empty list with the
// switch on; a file that does not parse or validate gives the same and the
// error, and is replaced only by the next change.
func Open(dir string) (*Engine, error) {
	e := &Engine{path: filepath.Join(dir, FileName), data: dir}
	e.cur.Store(&state{set: Set{Version: Version, Enabled: true, Rules: []Rule{}}})
	raw, err := os.ReadFile(e.path)
	if errors.Is(err, os.ErrNotExist) {
		return e, nil
	}
	if err != nil {
		return e, err
	}
	var s Set
	if err := json.Unmarshal(raw, &s); err != nil {
		return e, fmt.Errorf("%s: %w", e.path, err)
	}
	if s.Version > Version {
		return e, fmt.Errorf("%s: version %d is newer than this FormNET reads (%d)", e.path, s.Version, Version)
	}
	st, err := e.build(s.Enabled, s.Rules)
	if err != nil {
		return e, fmt.Errorf("%s: %w", e.path, err)
	}
	e.cur.Store(st)
	return e, nil
}

// Current returns the list as kept.
func (e *Engine) Current() Set {
	s := e.cur.Load().set
	s.Rules = append([]Rule{}, s.Rules...)
	return s
}

// Compiled returns the compiled rules and the global switch.
func (e *Engine) Compiled() ([]*Compiled, bool) {
	st := e.cur.Load()
	return st.compiled, st.set.Enabled
}

// Replace validates the whole list, writes it and puts it in force.
func (e *Engine) Replace(list []Rule) (Set, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	st, err := e.build(e.cur.Load().set.Enabled, list)
	if err != nil {
		return Set{}, err
	}
	return e.commit(st)
}

// SetEnabled turns every rule on or off at once, and writes the file.
func (e *Engine) SetEnabled(on bool) (Set, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	cur := e.cur.Load()
	st := &state{set: cur.set, compiled: cur.compiled}
	st.set.Enabled = on
	return e.commit(st)
}

func (e *Engine) commit(st *state) (Set, error) {
	st.set.Version = Version
	raw, err := json.MarshalIndent(st.set, "", "  ")
	if err != nil {
		return Set{}, err
	}
	if err := userfile.Write(e.path, append(raw, '\n')); err != nil {
		return Set{}, err
	}
	e.cur.Store(st)
	return e.Current(), nil
}

func (e *Engine) build(enabled bool, list []Rule) (*state, error) {
	if len(list) > MaxRules {
		return nil, fieldErr("rules", "at most %d rules", MaxRules)
	}
	st := &state{set: Set{Version: Version, Enabled: enabled, Rules: make([]Rule, 0, len(list))}}
	seen := map[string]bool{}
	for i, r := range list {
		field := fmt.Sprintf("rules[%d]", i)
		r.ID = strings.TrimSpace(r.ID)
		if r.ID == "" {
			r.ID = newID()
		}
		if len(r.ID) > 64 || strings.ContainsAny(r.ID, "/\\ \t\r\n") {
			return nil, fieldErr(field+".id", "must be at most 64 characters without spaces or slashes")
		}
		if seen[r.ID] {
			return nil, fieldErr(field+".id", "%q is used by another rule", r.ID)
		}
		seen[r.ID] = true
		r.Name = strings.TrimSpace(r.Name)
		if r.Name == "" {
			return nil, fieldErr(field+".name", "is required")
		}
		if len(r.Name) > 200 || strings.ContainsAny(r.Name, "\r\n") {
			return nil, fieldErr(field+".name", "must be one line of at most 200 characters")
		}
		c, err := e.compile(r, field)
		if err != nil {
			return nil, err
		}
		st.set.Rules = append(st.set.Rules, c.Rule)
		st.compiled = append(st.compiled, c)
	}
	return st, nil
}

func (e *Engine) compile(r Rule, field string) (*Compiled, error) {
	m, err := Compile(r.Match, field+".match")
	if err != nil {
		return nil, err
	}
	c := &Compiled{Rule: r, Matcher: m}
	a := &c.Action
	af := field + ".action"
	a.Type = strings.ToLower(strings.TrimSpace(a.Type))
	switch a.Type {
	case ActionRewrite:
		if len(a.RequestHeaders)+len(a.ResponseHeaders)+len(a.Query)+len(a.Body) == 0 && a.Host == "" && a.Path == "" && a.Status == 0 {
			return nil, fieldErr(af, "a rewrite needs at least one change")
		}
		for i, h := range a.RequestHeaders {
			if err := checkHeaderOp(h, fmt.Sprintf("%s.request_headers[%d]", af, i)); err != nil {
				return nil, err
			}
			a.RequestHeaders[i].Op = strings.ToLower(strings.TrimSpace(h.Op))
		}
		for i, h := range a.ResponseHeaders {
			if err := checkHeaderOp(h, fmt.Sprintf("%s.response_headers[%d]", af, i)); err != nil {
				return nil, err
			}
			a.ResponseHeaders[i].Op = strings.ToLower(strings.TrimSpace(h.Op))
		}
		if a.Host != "" {
			a.Host = strings.ToLower(strings.TrimSpace(a.Host))
			if u, err := url.Parse("http://" + a.Host + "/"); err != nil || u.Hostname() == "" || u.Path != "/" || u.User != nil {
				return nil, fieldErr(af+".host", "must be a host name, optionally with :port")
			}
		}
		if a.Path != "" && !strings.HasPrefix(a.Path, "/") {
			return nil, fieldErr(af+".path", "must start with /")
		}
		if strings.ContainsAny(a.Path, "?# \t\r\n") {
			return nil, fieldErr(af+".path", "must be a path without a query, fragment or spaces")
		}
		for i, q := range a.Query {
			op := strings.ToLower(strings.TrimSpace(q.Op))
			if op != "set" && op != "remove" {
				return nil, fieldErr(fmt.Sprintf("%s.query[%d].op", af, i), "must be set or remove")
			}
			if q.Name == "" {
				return nil, fieldErr(fmt.Sprintf("%s.query[%d].name", af, i), "is required")
			}
			a.Query[i].Op = op
		}
		if a.Status != 0 && (a.Status < 100 || a.Status > 999) {
			return nil, fieldErr(af+".status", "must be an HTTP status between 100 and 999")
		}
		for i, b := range a.Body {
			bf := fmt.Sprintf("%s.body[%d]", af, i)
			t := strings.ToLower(strings.TrimSpace(b.Target))
			if t == "" {
				t = "response"
			}
			if t != "request" && t != "response" {
				return nil, fieldErr(bf+".target", "must be request or response")
			}
			a.Body[i].Target = t
			if b.Find == "" {
				return nil, fieldErr(bf+".find", "is required")
			}
			if len(b.Find) > 4096 {
				return nil, fieldErr(bf+".find", "may be at most 4096 characters")
			}
			pattern := regexp.QuoteMeta(b.Find)
			if b.Regex {
				pattern = b.Find
			}
			re, err := regexp.Compile(pattern)
			if err != nil {
				return nil, fieldErr(bf+".find", "is not a valid regular expression: %v", err)
			}
			c.finds = append(c.finds, re)
		}
	case ActionMapRemote:
		u, err := url.Parse(strings.TrimSpace(a.To))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return nil, fieldErr(af+".to", "must be an http or https URL such as https://staging.example.com:8443/v2, without a query")
		}
		c.To = u
		a.To = u.String()
	case ActionMapLocal:
		p, err := e.checkLocal(a.LocalPath)
		if err != nil {
			return nil, fieldErr(af+".local_path", "%s", err.Error())
		}
		a.LocalPath = p
	case ActionBlock:
	case ActionBreakpoint:
		if !a.Request && !a.Response {
			return nil, fieldErr(af, "a breakpoint pauses at the request, the response or both: set request or response to true")
		}
	case "":
		return nil, fieldErr(af+".type", "is required: rewrite, map_remote, map_local, block or breakpoint")
	default:
		return nil, fieldErr(af+".type", "must be rewrite, map_remote, map_local, block or breakpoint, not %q", a.Type)
	}
	c.Rule.Action = *a
	return c, nil
}

func checkHeaderOp(h HeaderOp, field string) error {
	switch strings.ToLower(strings.TrimSpace(h.Op)) {
	case "add", "set", "remove":
	default:
		return fieldErr(field+".op", "must be add, set or remove")
	}
	if !isToken(h.Name) {
		return fieldErr(field+".name", "%q is not a header name", h.Name)
	}
	if strings.ContainsAny(h.Value, "\r\n\x00") {
		return fieldErr(field+".value", "holds a line break")
	}
	return nil
}

// checkLocal resolves a map_local path: absolute, existing, and neither
// FormNET's data folder nor anything under it.
func (e *Engine) checkLocal(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", errors.New("is required: the file or folder to answer from")
	}
	if !filepath.IsAbs(p) {
		return "", errors.New("must be an absolute path")
	}
	p = filepath.Clean(p)
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", fmt.Errorf("%s cannot be read: %v", p, err)
	}
	if e.UnderData(real) || e.UnderData(p) {
		return "", errors.New("is FormNET's data folder or inside it, which holds the root certificate's key and the session token; FormNET never serves it")
	}
	return p, nil
}

// UnderData reports whether p is the data folder or inside it, symlinks resolved where they exist.
func (e *Engine) UnderData(p string) bool {
	roots := []string{filepath.Clean(e.data)}
	if real, err := filepath.EvalSymlinks(e.data); err == nil {
		roots = append(roots, real)
	}
	for _, root := range roots {
		if Within(p, root) {
			return true
		}
	}
	return false
}

// Within reports whether p is root or a path under it, on cleaned paths; on
// macOS and Windows, whose usual file systems ignore case, case is ignored.
func Within(p, root string) bool {
	p, root = filepath.Clean(p), filepath.Clean(root)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		p, root = strings.ToLower(p), strings.ToLower(root)
	}
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
}

func newID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return "r" + hex.EncodeToString(b[:])
}

// Plan is what the matching rules ask for one request, in list order.
type Plan struct {
	Names     []string    // every rule that matched, by name
	Rewrites  []*Compiled // rewrite rules, applied in order
	MapRemote *Compiled   // the first map_remote
	Terminal  *Compiled   // the first block or map_local: it answers instead of the server
	BreakReq  *Compiled   // the first breakpoint that pauses the request
	BreakResp *Compiled   // the first breakpoint that pauses the response
}

// PlanFor returns the plan of a request, or nil when no enabled rule matches
// or the global switch is off. The walk stops at the first block or map_local.
func (e *Engine) PlanFor(r Request) *Plan {
	st := e.cur.Load()
	if !st.set.Enabled {
		return nil
	}
	var p *Plan
	for _, c := range st.compiled {
		if !c.Enabled || !c.Matcher.Match(r) {
			continue
		}
		if p == nil {
			p = &Plan{}
		}
		p.Names = append(p.Names, c.Name)
		switch c.Action.Type {
		case ActionRewrite:
			p.Rewrites = append(p.Rewrites, c)
		case ActionMapRemote:
			if p.MapRemote == nil {
				p.MapRemote = c
			}
		case ActionBreakpoint:
			if c.Action.Request && p.BreakReq == nil {
				p.BreakReq = c
			}
			if c.Action.Response && p.BreakResp == nil {
				p.BreakResp = c
			}
		case ActionBlock, ActionMapLocal:
			p.Terminal = c
			return p
		}
	}
	return p
}

// ApplyHeaders runs header operations on h in order.
func ApplyHeaders(h http.Header, ops []HeaderOp) {
	for _, op := range ops {
		switch op.Op {
		case "add":
			h.Add(op.Name, op.Value)
		case "set":
			h.Set(op.Name, op.Value)
		case "remove":
			h.Del(op.Name)
		}
	}
}

// ApplyURL changes host, path and query parameters of u as a rewrite says.
func (c *Compiled) ApplyURL(u *url.URL) bool {
	a := c.Action
	changed := false
	if a.Host != "" {
		u.Host = a.Host
		changed = true
	}
	if a.Path != "" {
		u.Path, u.RawPath = a.Path, ""
		changed = true
	}
	if len(a.Query) > 0 {
		q := u.Query()
		for _, op := range a.Query {
			if op.Op == "set" {
				q.Set(op.Name, op.Value)
			} else {
				q.Del(op.Name)
			}
		}
		u.RawQuery = q.Encode()
		changed = true
	}
	return changed
}

// HasBody reports whether the rewrite changes the body of target (request or response).
func (c *Compiled) HasBody(target string) bool {
	for _, b := range c.Action.Body {
		if b.Target == target {
			return true
		}
	}
	return false
}

// ReplaceBody applies the rewrite's find and replace for target to a decoded text body.
func (c *Compiled) ReplaceBody(target string, body []byte) []byte {
	for i, b := range c.Action.Body {
		if b.Target != target {
			continue
		}
		if b.Regex {
			body = c.finds[i].ReplaceAll(body, []byte(b.Replace))
		} else {
			body = c.finds[i].ReplaceAllLiteral(body, []byte(b.Replace))
		}
	}
	return body
}

// MapURL returns where map_remote sends u: the target's scheme, host and port,
// and its path in place of the match's literal path prefix.
func (c *Compiled) MapURL(u *url.URL) *url.URL {
	out := *u
	out.Scheme = c.To.Scheme
	out.Host = c.To.Host
	if to := c.To.Path; to != "" {
		rest := strings.TrimPrefix(u.Path, c.Matcher.PathPrefix())
		switch {
		case strings.HasSuffix(to, "/") && strings.HasPrefix(rest, "/"):
			rest = rest[1:]
		case !strings.HasSuffix(to, "/") && rest != "" && !strings.HasPrefix(rest, "/"):
			rest = "/" + rest
		}
		out.Path, out.RawPath = to+rest, ""
	}
	return &out
}

// IsText reports whether a content type names a text body a find and replace may change.
func IsText(contentType string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	switch {
	case ct == "":
		return true
	case strings.HasPrefix(ct, "text/"),
		strings.HasSuffix(ct, "+json"), strings.HasSuffix(ct, "+xml"),
		ct == "application/json", ct == "application/xml", ct == "application/javascript",
		ct == "application/ecmascript", ct == "application/x-javascript", ct == "application/x-www-form-urlencoded",
		ct == "application/graphql", ct == "image/svg+xml", ct == "application/x-ndjson":
		return true
	}
	return false
}
