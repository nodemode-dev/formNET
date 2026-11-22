package proxy

import (
	"context"
	"fmt"
	"time"

	"github.com/nodemode-dev/formnet/internal/flow"
)

// Status reports the proxy's state.
func (p *Proxy) Status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.status
	s.Intercept = p.intercept
	s.InterceptMode = p.mode
	s.Passthrough = append([]string{}, p.passthrough...)
	return s
}

// Interception modes: auto opens HTTPS while the root is trusted, on always, off never.
const (
	ModeAuto = "auto"
	ModeOn   = "on"
	ModeOff  = "off"
)

// SetIntercept turns TLS interception on or off for new connections.
func (p *Proxy) SetIntercept(on bool) {
	mode := ModeOff
	if on {
		mode = ModeOn
	}
	_ = p.SetInterceptMode(mode)
}

// SetInterceptMode sets auto, on or off for new connections.
func (p *Proxy) SetInterceptMode(mode string) error {
	if mode != ModeAuto && mode != ModeOn && mode != ModeOff {
		return fmt.Errorf("intercept must be auto, on or off, not %q", mode)
	}
	on := mode == ModeOn || (mode == ModeAuto && p.trustedNow())
	p.mu.Lock()
	p.mode, p.intercept = mode, on
	p.mu.Unlock()
	return nil
}

func (p *Proxy) trustedNow() bool {
	return p.opts.Trusted != nil && p.opts.Trusted()
}

// followTrust reads the root's trust state again on every tick while the
// mode is auto, so trusting or removing the root takes effect without a restart.
func (p *Proxy) followTrust(ctx context.Context) {
	if p.opts.Trusted == nil {
		return
	}
	t := time.NewTicker(p.opts.TrustInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		p.mu.Lock()
		auto := p.mode == ModeAuto
		p.mu.Unlock()
		if !auto {
			continue
		}
		on := p.trustedNow()
		p.mu.Lock()
		changed := p.mode == ModeAuto && p.intercept != on
		if changed {
			p.intercept = on
		}
		p.mu.Unlock()
		switch {
		case changed && on:
			p.log.Info("the root certificate is trusted now, so HTTPS is opened")
		case changed:
			p.log.Info("the root certificate is no longer trusted, so HTTPS is counted but not opened")
		}
	}
}

// SetPassthrough replaces the host patterns that are tunnelled without interception.
func (p *Proxy) SetPassthrough(patterns []string) {
	p.mu.Lock()
	p.passthrough = append([]string(nil), patterns...)
	p.mu.Unlock()
}

// shouldIntercept decides whether a CONNECT to host is opened or tunnelled.
func (p *Proxy) shouldIntercept(host string) bool {
	if p.opts.CA == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.intercept {
		return false
	}
	for _, pattern := range p.passthrough {
		if flow.HostMatches(host, pattern) {
			return false
		}
	}
	return true
}

func (p *Proxy) setRunning(addr string, running bool) {
	p.mu.Lock()
	p.status.Addr = addr
	p.status.Running = running
	if running {
		p.status.Error = ""
	}
	p.mu.Unlock()
}

func (p *Proxy) setError(err error) {
	p.mu.Lock()
	p.status.Running = false
	p.status.Error = err.Error()
	p.mu.Unlock()
}
