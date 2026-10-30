// Package proxy is the HTTP(S) proxy formnet records through. A program
// pointed at it (a browser, curl, an app that honours proxy settings) sends
// plain requests and CONNECT tunnels; the proxy forwards them, and with the
// local CA trusted it opens the TLS inside the tunnel and records the exchange
// as a flow. Hosts on the passthrough list, and every host while interception
// is off, are tunnelled as they are and only counted.
package proxy

import (
	"context"
	"errors"
	"log/slog"

	"github.com/nodemode-dev/formnet/internal/ca"
	"github.com/nodemode-dev/formnet/internal/flow"
)

// Status is what the API reports about the proxy.
type Status struct {
	Addr        string   `json:"addr"`
	Running     bool     `json:"running"`
	Intercept   bool     `json:"intercept"`
	Passthrough []string `json:"passthrough"`
	Error       string   `json:"error,omitempty"`
}

// Options configure a Proxy.
type Options struct {
	CA          *ca.CA
	Store       *flow.Store
	BodyLimit   int64
	Passthrough []string
	Intercept   bool
	// Owner names the local program behind a client address (ip:port), when known.
	Owner func(clientAddr string) (pid int, name string, ok bool)
	Log   *slog.Logger
}

// Proxy is one running proxy.
type Proxy struct{}

var errNotImplemented = errors.New("proxy: not implemented")

// New prepares a proxy; ListenAndServe starts it.
func New(opts Options) *Proxy { return &Proxy{} }

// ListenAndServe serves on addr until ctx ends.
func (p *Proxy) ListenAndServe(ctx context.Context, addr string) error { return errNotImplemented }

// Status reports the proxy's state.
func (p *Proxy) Status() Status { return Status{} }

// SetIntercept turns TLS interception on or off for new connections.
func (p *Proxy) SetIntercept(on bool) {}

// SetPassthrough replaces the host patterns that are tunnelled without interception.
func (p *Proxy) SetPassthrough(patterns []string) {}
