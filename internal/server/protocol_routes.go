package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/nodemode-dev/formnet/internal/flow"
	"github.com/nodemode-dev/formnet/internal/proxy"
	"github.com/nodemode-dev/formnet/internal/rules"
)

// ProtocolControl is what the API needs for WebSocket messages and the upstream proxy.
type ProtocolControl interface {
	Messages(flowID string, since uint64, limit int) (proxy.MessagePage, bool)
	SubscribeMessages(ctx context.Context) <-chan proxy.MessageEvent
	Upstream() proxy.UpstreamStatus
	SetUpstream(c proxy.UpstreamChange) (proxy.UpstreamStatus, error)
}

func (s *handler) upstreamStatus() *proxy.UpstreamStatus {
	if s.Protocol == nil {
		return nil
	}
	st := s.Protocol.Upstream()
	return &st
}

func (s *handler) messageEvents(ctx context.Context) <-chan proxy.MessageEvent {
	if s.Protocol == nil {
		return nil
	}
	return s.Protocol.SubscribeMessages(ctx)
}

// registerProtocol adds the WebSocket messages route and the upstream proxy routes.
func (s *handler) registerProtocol(mux *http.ServeMux) {
	if s.Protocol == nil {
		return
	}
	pc := s.Protocol
	mux.HandleFunc("GET /api/flows/{id}/messages", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		f, ok := s.Store.Get(id)
		if !ok {
			writeError(w, http.StatusNotFound, "no such flow")
			return
		}
		if f.Kind != flow.KindWebSocket {
			writeError(w, http.StatusConflict, "this flow is not a WebSocket")
			return
		}
		var since uint64
		if v := r.URL.Query().Get("since"); v != "" {
			n, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				writeError(w, http.StatusBadRequest, "since must be a message seq, a non-negative number")
				return
			}
			since = n
		}
		limit := 0
		if v := r.URL.Query().Get("limit"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				writeError(w, http.StatusBadRequest, "limit must be a non-negative number")
				return
			}
			limit = n
		}
		page, kept := pc.Messages(id, since, limit)
		if !kept && f.Messages != nil {
			page.MessageSummary = *f.Messages
			page.Evicted = f.Messages.Count > 0
			page.Next = f.Messages.Count
		}
		writeJSON(w, http.StatusOK, page)
	})
	mux.HandleFunc("GET /api/upstream", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, pc.Upstream()) })
	mux.HandleFunc("POST /api/upstream", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Enabled     *bool     `json:"enabled"`
			URL         *string   `json:"url"`
			Bypass      *[]string `json:"bypass"`
			Username    *string   `json:"username"`
			Password    *string   `json:"password"`
			HasPassword *bool     `json:"has_password"`
		}
		if !decodeStrict(w, r, 64<<10, &in, `body must be JSON: {"enabled", "url", "bypass", "username", "password"} (each optional)`) {
			return
		}
		st, err := pc.SetUpstream(proxy.UpstreamChange{Enabled: in.Enabled, URL: in.URL, Bypass: in.Bypass, Username: in.Username, Password: in.Password})
		var field *rules.FieldError
		switch {
		case err == nil:
			writeJSON(w, http.StatusOK, st)
		case errors.As(err, &field):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
	})
}
