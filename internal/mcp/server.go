// Package mcp is FormNET's MCP server on stdio. It speaks protocol 2026-07-28
// (every request carries its version and capabilities in _meta, no session)
// and answers the initialize handshake of 2025-11-25 and earlier for clients
// that still send it. Each tool call reads <data>/endpoints.json and
// <data>/token again and talks to the running FormNET's loopback API; the
// server starts no program and never reads the root's private key.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
)

const (
	// Modern is the protocol revision served statelessly.
	Modern     = "2026-07-28"
	metaPrefix = "io.modelcontextprotocol/"
	maxMessage = 4 << 20
	listTTL    = 3600000
)

// Legacy are the handshake revisions answered for clients that send initialize.
var Legacy = []string{"2025-11-25", "2025-06-18", "2025-03-26"}

const (
	codeParse       = -32700
	codeInvalid     = -32600
	codeNoMethod    = -32601
	codeParams      = -32602
	codeInternal    = -32603
	codeUnsupported = -32022
)

const instructions = "FormNET records what programs on this computer send and receive: an intercepting HTTP(S) proxy and a table of every connection with its process. " +
	"These tools read the running FormNET on loopback; when it is not running they say so, and the person starts it. " +
	"Start with proxy_status to learn the proxy address and how to point a program at it, then list_flows (newest last, small pages) and get_flow for one exchange. " +
	"Flow ids and the seq cursor are the only handles: pass a seq back as since to read what arrived after it. " +
	"Credential headers are redacted in every answer. export_har writes a file under <data>/exports only, redacted unless include_secrets is set. " +
	"clear_flows, set_interception and set_passthrough change the recording or the proxy: ask the person first. " +
	"Recorded traffic holds other programs' secrets: never send it, an export or the session token to another computer or service."

// Server answers MCP requests from one stdio client.
type Server struct {
	Data    string // the FormNET data folder: endpoints.json, token, exports/
	Version string
	Log     io.Writer

	tools  []*Tool
	byName map[string]*Tool

	mu      sync.Mutex
	out     *bufio.Writer
	pending map[string]*inflight
}

type inflight struct {
	cancel    context.CancelFunc
	cancelled bool
}

// New builds a server for the data folder.
func New(data, version string, log io.Writer) *Server {
	if log == nil {
		log = io.Discard
	}
	s := &Server{Data: data, Version: version, Log: log, byName: map[string]*Tool{}, pending: map[string]*inflight{}}
	for _, t := range registry() {
		s.tools = append(s.tools, t)
		s.byName[t.Name] = t
	}
	return s
}

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Serve reads newline-delimited JSON-RPC from in and writes only protocol
// messages to out, until in ends or ctx is done.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	s.out = bufio.NewWriter(out)
	rd := bufio.NewReaderSize(in, 64<<10)
	var wg sync.WaitGroup
	slots := make(chan struct{}, 8)
	defer wg.Wait()
	for {
		line, tooLong, err := readLine(rd)
		if tooLong {
			fmt.Fprintln(s.Log, "formnet mcp: dropped a message over 4 MB")
			s.reply(nil, nil, &rpcError{Code: codeInvalid, Message: "messages can be up to 4 MB"})
		} else if msg := bytes.TrimSpace(line); len(msg) > 0 {
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				return ctx.Err()
			}
			wg.Add(1)
			go func(msg []byte) {
				defer func() { <-slots; wg.Done() }()
				s.handle(ctx, msg)
			}(msg)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
}

// readLine returns one line without its newline. A line over maxMessage is
// read to its end and reported as too long.
func readLine(rd *bufio.Reader) ([]byte, bool, error) {
	var buf []byte
	tooLong := false
	for {
		chunk, err := rd.ReadSlice('\n')
		if !tooLong {
			if len(buf)+len(chunk) > maxMessage+1 {
				tooLong, buf = true, nil
			} else {
				buf = append(buf, chunk...)
			}
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return bytes.TrimRight(buf, "\r\n"), tooLong, err
	}
}

func (s *Server) write(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		fmt.Fprintln(s.Log, "formnet mcp: encode:", err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.out.Write(b)
	s.out.WriteByte('\n')
	s.out.Flush()
}

func (s *Server) reply(id json.RawMessage, result any, e *rpcError) {
	resp := map[string]any{"jsonrpc": "2.0"}
	if len(id) == 0 {
		resp["id"] = nil
	} else {
		resp["id"] = id
	}
	if e != nil {
		resp["error"] = e
	} else {
		resp["result"] = result
	}
	s.write(resp)
}

func (s *Server) handle(ctx context.Context, raw []byte) {
	if raw[0] == '[' {
		s.reply(nil, nil, &rpcError{Code: codeInvalid, Message: "batches are not supported; send one message per line"})
		return
	}
	var m message
	if err := json.Unmarshal(raw, &m); err != nil {
		s.reply(nil, nil, &rpcError{Code: codeParse, Message: "not JSON"})
		return
	}
	hasID := len(m.ID) > 0 && string(m.ID) != "null"
	if m.JSONRPC != "2.0" || (m.Method == "" && (m.Result == nil && m.Error == nil)) {
		if hasID || m.Method == "" {
			s.reply(m.ID, nil, &rpcError{Code: codeInvalid, Message: "not a JSON-RPC 2.0 message"})
		}
		return
	}
	if m.Method == "" {
		return // a response from the client; this server sends no requests
	}
	if !hasID {
		if string(m.ID) == "null" {
			s.reply(nil, nil, &rpcError{Code: codeInvalid, Message: "a request id must not be null"})
			return
		}
		s.notify(m)
		return
	}
	key := string(m.ID)
	cctx, cancel := context.WithCancel(ctx)
	f := &inflight{cancel: cancel}
	s.mu.Lock()
	s.pending[key] = f
	s.mu.Unlock()
	result, rerr := s.dispatch(cctx, m)
	cancel()
	s.mu.Lock()
	delete(s.pending, key)
	dropped := f.cancelled
	s.mu.Unlock()
	if dropped {
		return
	}
	s.reply(m.ID, result, rerr)
}

func (s *Server) notify(m message) {
	if m.Method != "notifications/cancelled" {
		return
	}
	var p struct {
		RequestID json.RawMessage `json:"requestId"`
	}
	if json.Unmarshal(m.Params, &p) != nil || len(p.RequestID) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if f, ok := s.pending[string(p.RequestID)]; ok {
		f.cancelled = true
		f.cancel()
	}
}

func supported() []string { return append([]string{Modern}, Legacy...) }

func (s *Server) serverInfo() map[string]any {
	return map[string]any{"name": "formnet", "title": "FormNET", "version": s.Version}
}

func capabilities() map[string]any {
	return map[string]any{"tools": map[string]any{"listChanged": false}}
}

// dispatch serves one request. A request whose _meta carries a protocol
// version is served statelessly; one without is a handshake-era request.
func (s *Server) dispatch(ctx context.Context, m message) (any, *rpcError) {
	var params struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	if len(m.Params) > 0 && string(m.Params) != "null" {
		if err := json.Unmarshal(m.Params, &params); err != nil {
			return nil, &rpcError{Code: codeParams, Message: "params must be an object"}
		}
	}
	version, modern := "", false
	if raw, ok := params.Meta[metaPrefix+"protocolVersion"]; ok {
		if json.Unmarshal(raw, &version) != nil || version == "" {
			return nil, &rpcError{Code: codeParams, Message: metaPrefix + "protocolVersion must be a string"}
		}
		if !slices.Contains(supported(), version) {
			return nil, &rpcError{Code: codeUnsupported, Message: "Unsupported protocol version", Data: map[string]any{"supported": supported(), "requested": version}}
		}
		modern = version == Modern
		if caps, ok := params.Meta[metaPrefix+"clientCapabilities"]; modern && (!ok || len(caps) == 0 || caps[0] != '{') {
			return nil, &rpcError{Code: codeParams, Message: "requests of protocol " + Modern + " must carry " + metaPrefix + "clientCapabilities in _meta"}
		}
	}
	result, rerr := s.method(ctx, m, modern)
	if rerr != nil || !modern {
		return result, rerr
	}
	result["resultType"] = "complete"
	result["_meta"] = map[string]any{metaPrefix + "serverInfo": s.serverInfo()}
	return result, nil
}

func (s *Server) method(ctx context.Context, m message, modern bool) (map[string]any, *rpcError) {
	switch m.Method {
	case "server/discover":
		return map[string]any{"supportedVersions": supported(), "capabilities": capabilities(), "instructions": instructions, "ttlMs": listTTL, "cacheScope": "public"}, nil
	case "initialize":
		if modern {
			return nil, &rpcError{Code: codeNoMethod, Message: "initialize is not part of protocol " + Modern + "; send requests with _meta directly"}
		}
		var in struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(m.Params, &in)
		v := Legacy[0]
		if slices.Contains(Legacy, in.ProtocolVersion) {
			v = in.ProtocolVersion
		}
		return map[string]any{"protocolVersion": v, "capabilities": capabilities(), "serverInfo": s.serverInfo(), "instructions": instructions}, nil
	case "ping":
		if modern {
			return nil, &rpcError{Code: codeNoMethod, Message: "ping is not part of protocol " + Modern}
		}
		return map[string]any{}, nil
	case "tools/list":
		out := map[string]any{"tools": Definitions()}
		if modern {
			out["ttlMs"] = listTTL
			out["cacheScope"] = "public"
		}
		return out, nil
	case "tools/call":
		var in struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(m.Params, &in); err != nil || in.Name == "" {
			return nil, &rpcError{Code: codeParams, Message: "tools/call needs a tool name and arguments"}
		}
		t := s.byName[in.Name]
		if t == nil {
			return nil, &rpcError{Code: codeParams, Message: "Unknown tool: " + in.Name}
		}
		return s.run(ctx, t, in.Arguments), nil
	}
	return nil, &rpcError{Code: codeNoMethod, Message: "Method not found: " + m.Method}
}

func (s *Server) run(ctx context.Context, t *Tool, raw json.RawMessage) map[string]any {
	a, err := check(t.Props, t.Required, raw)
	if err != nil {
		return errorResult(t.Name + ": " + err.Error())
	}
	structured, err := t.Run(ctx, s, a)
	if err != nil {
		var te *toolError
		if errors.As(err, &te) {
			return errorResult(te.msg)
		}
		if errors.Is(err, context.Canceled) {
			return errorResult("cancelled")
		}
		fmt.Fprintf(s.Log, "formnet mcp: %s: %v\n", t.Name, err)
		return errorResult(t.Name + " could not finish: " + err.Error())
	}
	text, err := json.MarshalIndent(structured, "", "  ")
	if err != nil {
		return errorResult(t.Name + " could not encode its answer")
	}
	return map[string]any{"content": []map[string]any{{"type": "text", "text": string(text)}}, "structuredContent": structured, "isError": false}
}

func errorResult(msg string) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": msg}}, "isError": true}
}
