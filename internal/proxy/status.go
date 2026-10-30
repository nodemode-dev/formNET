package proxy

import "github.com/nodemode-dev/formnet/internal/flow"

// Status reports the proxy's state.
func (p *Proxy) Status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.status
	s.Intercept = p.intercept
	s.Passthrough = append([]string{}, p.passthrough...)
	return s
}

// SetIntercept turns TLS interception on or off for new connections.
func (p *Proxy) SetIntercept(on bool) {
	p.mu.Lock()
	p.intercept = on
	p.mu.Unlock()
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
