package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	endpointsFile = "endpoints.json"
	tokenFile     = "token"
	product       = "formnet"
	maxAnswer     = 32 << 20
	startHint     = "Start FormNET (open the FormNET app, or run `formnet serve` in a terminal) and call this tool again. This MCP server never starts FormNET itself."
)

// running is what one call learns about the FormNET it talks to. It is read
// again for every call, so a restart, a moved port or a new token is followed.
type running struct {
	Addr      string `json:"addr"`
	ProxyAddr string `json:"proxy_addr"`
	CAPath    string `json:"ca_path"`
	PID       int    `json:"pid"`
	Version   string `json:"version"`
	token     string
}

// toolError is a failure the model should read and act on.
type toolError struct{ msg string }

func (e *toolError) Error() string { return e.msg }

func failf(format string, a ...any) error { return &toolError{fmt.Sprintf(format, a...)} }

var api = &http.Client{
	Timeout:   60 * time.Second,
	Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true},
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

func (s *Server) locate() (running, error) {
	raw, err := os.ReadFile(filepath.Join(s.Data, endpointsFile))
	if err != nil {
		return running{}, failf("FormNET is not running: there is no %s in %s. %s", endpointsFile, s.Data, startHint)
	}
	var r struct {
		running
		Product string `json:"product"`
	}
	if json.Unmarshal(raw, &r) != nil || r.Product != product {
		return running{}, failf("%s in %s is not a FormNET endpoints file. %s", endpointsFile, s.Data, startHint)
	}
	host, _, err := net.SplitHostPort(r.Addr)
	ip := net.ParseIP(host)
	if err != nil || ip == nil || !ip.IsLoopback() {
		return running{}, failf("%s names %q, which is not a loopback address; this MCP server talks to FormNET on loopback only", endpointsFile, r.Addr)
	}
	tok, err := os.ReadFile(filepath.Join(s.Data, tokenFile))
	if err != nil || strings.TrimSpace(string(tok)) == "" {
		return running{}, failf("FormNET's session token is not readable in %s; it is written by the running FormNET for the person who started it. %s", s.Data, startHint)
	}
	out := r.running
	out.token = strings.TrimSpace(string(tok))
	return out, nil
}

// call sends one request to the loopback API and decodes a JSON answer into out.
func (s *Server) call(ctx context.Context, method, path string, query url.Values, body any, out any) (running, error) {
	r, err := s.locate()
	if err != nil {
		return r, err
	}
	resp, err := s.send(ctx, r, method, path, query, body)
	if err != nil {
		return r, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer+1))
	if err != nil {
		return r, failf("the answer from FormNET was cut off: %v", err)
	}
	if len(raw) > maxAnswer {
		return r, failf("the answer from FormNET is over %d MB; narrow the request", maxAnswer>>20)
	}
	if out != nil && json.Unmarshal(raw, out) != nil {
		return r, failf("FormNET answered %s with something that is not JSON", path)
	}
	return r, nil
}

// send makes the request and turns a refused connection or an error status
// into a message that says what to do.
func (s *Server) send(ctx context.Context, r running, method, path string, query url.Values, body any) (*http.Response, error) {
	u := url.URL{Scheme: "http", Host: r.Addr, Path: path}
	if len(query) > 0 {
		u.RawQuery = query.Encode()
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = strings.NewReader(string(b))
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+r.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "formnet-mcp")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := api.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil, ctx.Err()
		}
		return nil, failf("FormNET is not answering at %s (the endpoints file may be left from a run that crashed). %s", r.Addr, startHint)
	}
	if resp.StatusCode/100 == 2 {
		return resp, nil
	}
	defer resp.Body.Close()
	var e struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&e)
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return nil, failf("FormNET at %s refused the session token in %s. It changes on every start: if FormNET was started with another data folder or FORMNET_TOKEN, start this MCP server with the same FORMNET_DATA.", r.Addr, filepath.Join(s.Data, tokenFile))
	case http.StatusNotFound:
		if e.Error == "" {
			e.Error = "not found"
		}
		return nil, failf("%s", e.Error)
	}
	if e.Error == "" {
		e.Error = resp.Status
	}
	return nil, failf("FormNET refused the request: %s", e.Error)
}
