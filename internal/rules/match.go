// Package rules is FormNET's matcher and rule engine: what a request looks
// like to a rule, the ordered rule list kept in <data>/rules.json, and the
// plan a request gets from it. The proxy carries the plan out.
package rules

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
)

// Match says which requests a rule or a throttle host entry is for. Every
// field but Host may be empty, which matches anything.
//
//	scheme  http or https
//	host    a glob, case-insensitive: * any run of characters, ? one; *.example.com also matches example.com
//	port    0 for any
//	path    a glob on the path without the query: /api/* (a * crosses slashes)
//	method  GET, or a list such as GET,POST; case-insensitive
//	query   name or name=glob pairs joined by &; each must be in the query
type Match struct {
	Scheme string `json:"scheme,omitempty"`
	Host   string `json:"host"`
	Port   int    `json:"port,omitempty"`
	Path   string `json:"path,omitempty"`
	Method string `json:"method,omitempty"`
	Query  string `json:"query,omitempty"`
}

// Request is what a matcher reads of one request.
type Request struct {
	Scheme string
	Host   string
	Port   int
	Path   string
	Method string
	Query  string // the raw query, without ?
}

// RequestOf reads a Request from a method and an absolute URL.
func RequestOf(method string, u *url.URL) Request {
	port := 0
	if p := u.Port(); p != "" {
		fmt.Sscan(p, &port)
	} else if u.Scheme == "https" {
		port = 443
	} else {
		port = 80
	}
	path := u.Path
	if path == "" {
		path = "/"
	}
	return Request{Scheme: u.Scheme, Host: u.Hostname(), Port: port, Path: path, Method: method, Query: u.RawQuery}
}

// Matcher is a compiled Match.
type Matcher struct {
	src     Match
	scheme  string
	host    *regexp.Regexp
	port    int
	path    *regexp.Regexp
	methods map[string]bool
	query   []queryPart
}

type queryPart struct {
	name  string
	value *regexp.Regexp // nil: the name alone must be present
}

// FieldError names the field a value is wrong in.
type FieldError struct {
	Field  string
	Reason string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Reason }

func fieldErr(field, format string, args ...any) error {
	return &FieldError{Field: field, Reason: fmt.Sprintf(format, args...)}
}

// Compile checks m and builds its matcher; field prefixes the field names in errors.
func Compile(m Match, field string) (*Matcher, error) {
	if field != "" {
		field += "."
	}
	out := &Matcher{src: m}
	switch s := strings.ToLower(strings.TrimSpace(m.Scheme)); s {
	case "", "http", "https":
		out.scheme = s
	default:
		return nil, fieldErr(field+"scheme", "must be http, https or empty, not %q", m.Scheme)
	}
	host := strings.ToLower(strings.TrimSpace(m.Host))
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if host == "" {
		return nil, fieldErr(field+"host", "is required (use * for every host)")
	}
	if strings.ContainsAny(host, "/\\ \t") || (strings.Contains(host, ":") && net.ParseIP(host) == nil) {
		return nil, fieldErr(field+"host", "must be a host name or pattern such as *.example.com, without a scheme, port or path")
	}
	if suffix, ok := strings.CutPrefix(host, "*."); ok && !strings.ContainsAny(suffix, "*?") {
		out.host = regexp.MustCompile(`^(?:.*\.)?` + regexp.QuoteMeta(suffix) + `$`)
	} else {
		out.host = regexp.MustCompile(`^` + globRegexp(host) + `$`)
	}
	if m.Port < 0 || m.Port > 65535 {
		return nil, fieldErr(field+"port", "must be between 1 and 65535, or 0 for any")
	}
	out.port = m.Port
	if p := strings.TrimSpace(m.Path); p != "" {
		if !strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "*") {
			return nil, fieldErr(field+"path", "must start with / or *")
		}
		out.path = regexp.MustCompile(`^` + globRegexp(p) + `$`)
	}
	if ms := strings.TrimSpace(m.Method); ms != "" {
		out.methods = map[string]bool{}
		for _, one := range strings.Split(ms, ",") {
			one = strings.ToUpper(strings.TrimSpace(one))
			if !isToken(one) {
				return nil, fieldErr(field+"method", "must be an HTTP method such as GET, or a list such as GET,POST")
			}
			out.methods[one] = true
		}
	}
	if q := strings.TrimPrefix(strings.TrimSpace(m.Query), "?"); q != "" {
		for _, pair := range strings.Split(q, "&") {
			if pair == "" {
				continue
			}
			name, value, hasValue := strings.Cut(pair, "=")
			name, err := url.QueryUnescape(name)
			if err != nil || name == "" {
				return nil, fieldErr(field+"query", "%q is not a name or name=value pair", pair)
			}
			part := queryPart{name: name}
			if hasValue {
				v, err := url.QueryUnescape(value)
				if err != nil {
					return nil, fieldErr(field+"query", "%q is not a name or name=value pair", pair)
				}
				part.value = regexp.MustCompile(`^` + globRegexp(v) + `$`)
			}
			out.query = append(out.query, part)
		}
	}
	return out, nil
}

// globRegexp turns a glob into a regular expression body: * is any run of
// characters, ? one character, everything else literal.
func globRegexp(glob string) string {
	var b strings.Builder
	for _, r := range glob {
		switch r {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	return b.String()
}

// Match reports whether r is one of the requests m is for.
func (m *Matcher) Match(r Request) bool {
	if m == nil {
		return false
	}
	if m.scheme != "" && !strings.EqualFold(m.scheme, r.Scheme) {
		return false
	}
	if m.port != 0 && m.port != r.Port {
		return false
	}
	if !m.host.MatchString(strings.TrimSuffix(strings.ToLower(strings.Trim(r.Host, "[]")), ".")) {
		return false
	}
	if m.methods != nil && !m.methods[strings.ToUpper(r.Method)] {
		return false
	}
	if m.path != nil {
		path := r.Path
		if path == "" {
			path = "/"
		}
		if !m.path.MatchString(path) {
			return false
		}
	}
	if len(m.query) > 0 {
		values, _ := url.ParseQuery(r.Query)
		for _, part := range m.query {
			got, ok := values[part.name]
			if !ok {
				return false
			}
			if part.value == nil {
				continue
			}
			found := false
			for _, v := range got {
				if part.value.MatchString(v) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}
	return true
}

// MatchHost reports whether the host pattern alone matches host.
func (m *Matcher) MatchHost(host string) bool {
	return m != nil && m.host.MatchString(strings.TrimSuffix(strings.ToLower(strings.Trim(host, "[]")), "."))
}

// PathPrefix is the literal start of the path pattern, up to its first
// wildcard; map_local and map_remote replace it.
func (m *Matcher) PathPrefix() string {
	p := strings.TrimSpace(m.src.Path)
	if i := strings.IndexAny(p, "*?"); i >= 0 {
		return p[:i]
	}
	return p
}

// LiteralHost is the host pattern when it holds no wildcard, else "".
func (m *Matcher) LiteralHost() string {
	h := strings.ToLower(strings.TrimSpace(m.src.Host))
	if strings.ContainsAny(h, "*?") {
		return ""
	}
	return h
}

func isToken(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0:
		default:
			return false
		}
	}
	return true
}
