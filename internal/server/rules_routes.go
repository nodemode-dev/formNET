package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/nodemode-dev/formnet/internal/flow"
	"github.com/nodemode-dev/formnet/internal/proxy"
	"github.com/nodemode-dev/formnet/internal/rules"
	"github.com/nodemode-dev/formnet/internal/throttle"
)

// TrafficControl is what the API needs for rules, breakpoints, the throttle
// and capture from other devices.
type TrafficControl interface {
	Rules() proxy.RulesStatus
	RulesSummary() proxy.RulesSummary
	ReplaceRules(list []rules.Rule) (proxy.RulesStatus, error)
	SetRulesEnabled(on bool) (proxy.RulesStatus, error)
	Breakpoints() proxy.BreakpointsStatus
	SetBreakpointSettings(timeoutSeconds, maxPaused *int) (proxy.BreakpointsStatus, error)
	ContinueBreakpoint(id string, edit proxy.BreakpointEdit) error
	AbortBreakpoint(id string) error
	SubscribeBreakpoints(ctx context.Context) <-chan proxy.BreakpointEvent
	Throttle() throttle.Status
	SetThrottle(c throttle.Config) (throttle.Status, error)
	Remote() proxy.RemoteStatus
	SetRemote(enabled bool, allow []string) (proxy.RemoteStatus, error)
}

// TrafficStatus is what /api/status adds about rules, the throttle and remote capture.
type TrafficStatus struct {
	Rules    proxy.RulesSummary `json:"rules"`
	Throttle throttle.Status    `json:"throttle"`
	Remote   proxy.RemoteStatus `json:"remote"`
}

func (s *handler) trafficStatus() *TrafficStatus {
	if s.Traffic == nil {
		return nil
	}
	return &TrafficStatus{Rules: s.Traffic.RulesSummary(), Throttle: s.Traffic.Throttle(), Remote: s.Traffic.Remote()}
}

func (s *handler) breakpointEvents(ctx context.Context) <-chan proxy.BreakpointEvent {
	if s.Traffic == nil {
		return nil
	}
	return s.Traffic.SubscribeBreakpoints(ctx)
}

// registerRules adds the routes of rules, breakpoints, the throttle and remote capture.
func (s *handler) registerRules(mux *http.ServeMux) {
	if s.Traffic == nil {
		return
	}
	t := s.Traffic
	mux.HandleFunc("GET /api/rules", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, t.Rules()) })
	mux.HandleFunc("PUT /api/rules", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Rules *[]rules.Rule `json:"rules"`
		}
		if !decodeStrict(w, r, 1<<20, &in, `body must be JSON: {"rules": [...]}, the whole list`) {
			return
		}
		if in.Rules == nil {
			writeError(w, http.StatusBadRequest, `rules: is required, the whole list ([] for none)`)
			return
		}
		st, err := t.ReplaceRules(*in.Rules)
		if err != nil {
			writeTrafficError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, st)
	})
	mux.HandleFunc("POST /api/rules/enabled", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Enabled *bool `json:"enabled"`
		}
		if !decodeStrict(w, r, 4<<10, &in, `body must be JSON: {"enabled": true} or {"enabled": false}`) {
			return
		}
		if in.Enabled == nil {
			writeError(w, http.StatusBadRequest, "enabled: is required, true or false")
			return
		}
		st, err := t.SetRulesEnabled(*in.Enabled)
		if err != nil {
			writeTrafficError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, st)
	})
	mux.HandleFunc("GET /api/breakpoints", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, t.Breakpoints()) })
	mux.HandleFunc("POST /api/breakpoints", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			TimeoutSeconds *int `json:"timeout_seconds"`
			MaxPaused      *int `json:"max_paused"`
		}
		if !decodeStrict(w, r, 4<<10, &in, `body must be JSON: {"timeout_seconds": 60, "max_paused": 20} (either field)`) {
			return
		}
		st, err := t.SetBreakpointSettings(in.TimeoutSeconds, in.MaxPaused)
		if err != nil {
			writeTrafficError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, st)
	})
	mux.HandleFunc("POST /api/breakpoints/{id}/continue", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Method     *string        `json:"method"`
			URL        *string        `json:"url"`
			Status     *int           `json:"status"`
			Headers    *[]flow.Header `json:"headers"`
			BodyText   *string        `json:"body_text"`
			BodyBase64 *string        `json:"body_base64"`
		}
		if !decodeStrict(w, r, 12<<20, &in, "body must be JSON with method, url, status, headers, body_text or body_base64 (each optional), or empty") {
			return
		}
		if in.BodyText != nil && in.BodyBase64 != nil {
			writeError(w, http.StatusBadRequest, "give body_text or body_base64, not both")
			return
		}
		edit := proxy.BreakpointEdit{Method: in.Method, URL: in.URL, Status: in.Status, Headers: in.Headers}
		if in.BodyText != nil {
			b := []byte(*in.BodyText)
			edit.Body = &b
		}
		if in.BodyBase64 != nil {
			b, err := base64.StdEncoding.DecodeString(*in.BodyBase64)
			if err != nil {
				writeError(w, http.StatusBadRequest, "body_base64: is not base64")
				return
			}
			edit.Body = &b
		}
		if err := t.ContinueBreakpoint(r.PathValue("id"), edit); err != nil {
			writeTrafficError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"continued": true})
	})
	mux.HandleFunc("POST /api/breakpoints/{id}/abort", func(w http.ResponseWriter, r *http.Request) {
		if err := t.AbortBreakpoint(r.PathValue("id")); err != nil {
			writeTrafficError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"aborted": true})
	})
	mux.HandleFunc("GET /api/throttle", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, t.Throttle()) })
	mux.HandleFunc("POST /api/throttle", func(w http.ResponseWriter, r *http.Request) {
		c := t.Throttle().Config
		if !decodeStrict(w, r, 64<<10, &c, "body must be JSON with enabled, preset, down_kbps, up_kbps, latency_ms or hosts (each optional)") {
			return
		}
		st, err := t.SetThrottle(c)
		if err != nil {
			writeTrafficError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, st)
	})
	mux.HandleFunc("GET /api/remote", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, t.Remote()) })
	mux.HandleFunc("POST /api/remote", func(w http.ResponseWriter, r *http.Request) {
		cur := t.Remote()
		in := struct {
			Enabled *bool     `json:"enabled"`
			Allow   *[]string `json:"allow"`
		}{}
		if !decodeStrict(w, r, 64<<10, &in, `body must be JSON: {"enabled": bool, "allow": ["192.168.1.0/24"]} (either field)`) {
			return
		}
		enabled, allow := cur.Enabled, cur.Allow
		if in.Enabled != nil {
			enabled = *in.Enabled
		}
		if in.Allow != nil {
			allow = *in.Allow
		}
		st, err := t.SetRemote(enabled, allow)
		if err != nil {
			writeTrafficError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, st)
	})
}

// decodeStrict reads a JSON body into v, refusing unknown fields; an empty body leaves v as it is.
func decodeStrict(w http.ResponseWriter, r *http.Request, limit int64, v any, usage string) bool {
	raw, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "the request body could not be read")
		return false
	}
	if int64(len(raw)) > limit {
		writeError(w, http.StatusRequestEntityTooLarge, "the request body is too large")
		return false
	}
	if len(raw) == 0 {
		return true
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) && typeErr.Field != "" {
			writeError(w, http.StatusBadRequest, typeErr.Field+": must be "+typeErr.Type.String())
			return false
		}
		writeError(w, http.StatusBadRequest, usage+" ("+err.Error()+")")
		return false
	}
	return true
}

func writeTrafficError(w http.ResponseWriter, err error) {
	var field *rules.FieldError
	var input *proxy.ResendInputError
	switch {
	case errors.Is(err, proxy.ErrNoBreakpoint):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, proxy.ErrTrafficUnavailable):
		writeError(w, http.StatusNotImplemented, err.Error())
	case errors.As(err, &field), errors.As(err, &input):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}
