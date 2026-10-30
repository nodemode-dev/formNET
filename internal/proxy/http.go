package proxy

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"sync"
	"time"

	"github.com/nodemode-dev/formnet/internal/flow"
)

// timing collects the client-side trace of one upstream exchange. The trace
// callbacks run on the transport's goroutines, so the fields are guarded.
type timing struct {
	mu                        sync.Mutex
	connectStart, connectDone time.Time
	tlsStart, tlsDone         time.Time
	wrote, firstByte          time.Time
}

func (t *timing) set(field *time.Time) {
	t.mu.Lock()
	*field = time.Now()
	t.mu.Unlock()
}

func (t *timing) trace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		ConnectStart:         func(string, string) { t.set(&t.connectStart) },
		ConnectDone:          func(string, string, error) { t.set(&t.connectDone) },
		TLSHandshakeStart:    func() { t.set(&t.tlsStart) },
		TLSHandshakeDone:     func(tls.ConnectionState, error) { t.set(&t.tlsDone) },
		WroteRequest:         func(httptrace.WroteRequestInfo) { t.set(&t.wrote) },
		GotFirstResponseByte: func() { t.set(&t.firstByte) },
	}
}

func millis(from, to time.Time) float64 {
	if from.IsZero() || to.IsZero() || to.Before(from) {
		return 0
	}
	return float64(to.Sub(from)) / float64(time.Millisecond)
}

func (t *timing) connectMS() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return millis(t.connectStart, t.connectDone) + millis(t.tlsStart, t.tlsDone)
}

func (t *timing) ttfbMS() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return millis(t.wrote, t.firstByte)
}

// forward relays one plain exchange and records it.
func (p *Proxy) forward(w http.ResponseWriter, r *http.Request, o origin) {
	rawURL := o.url(r)
	f := p.newFlow(flow.KindHTTP, o, r.Method, rawURL)
	reqCapture := newCapture(r.Body, p.opts.BodyLimit)
	requestLine := fmt.Sprintf("%s %s %s", r.Method, r.URL.RequestURI(), r.Proto)
	f.Request = reqCapture.message(r.Header, requestLine, p.opts.BodyLimit)
	f.ClientProtocol = r.Proto
	f.Protocol = r.Proto
	f = p.opts.Store.Add(f)

	out, err := p.outgoing(r, rawURL, reqCapture)
	if err != nil {
		p.fail(w, f, http.StatusBadRequest, "formnet could not build the upstream request: "+err.Error())
		return
	}
	tm := &timing{}
	out = out.WithContext(httptrace.WithClientTrace(out.Context(), tm.trace()))
	resp, err := p.transport.RoundTrip(out)
	if err != nil {
		p.fail(w, f, statusFor(err), "formnet could not reach "+f.Host+": "+err.Error())
		return
	}
	defer resp.Body.Close()
	f, copyErr := p.relayResponse(w, resp, f, tm, reqCapture, requestLine, r.Header)
	p.finish(f)
	if copyErr != nil {
		// The upstream stopped mid-body. Resetting the client connection is the
		// only honest signal that the body is not complete.
		panic(http.ErrAbortHandler)
	}
}

// outgoing builds the upstream request from the client's.
func (p *Proxy) outgoing(r *http.Request, rawURL string, body io.ReadCloser) (*http.Request, error) {
	out, err := http.NewRequestWithContext(r.Context(), r.Method, rawURL, body)
	if err != nil {
		return nil, err
	}
	out.Header = forwardable(r.Header, false)
	out.ContentLength = r.ContentLength
	if r.ContentLength == 0 && r.Body == http.NoBody {
		out.Body = http.NoBody
	}
	out.Host = r.Host
	if out.Host == "" {
		out.Host = out.URL.Host
	}
	return out, nil
}

// relayResponse copies the upstream response to the client, flushing as it
// goes so streams arrive as they are sent, and completes the record. A
// non-nil error means the upstream body was cut short after the headers went out.
func (p *Proxy) relayResponse(w http.ResponseWriter, resp *http.Response, f flow.Flow, tm *timing, reqCapture *capture, requestLine string, reqHeader http.Header) (flow.Flow, error) {
	resCapture := newCapture(resp.Body, p.opts.BodyLimit)
	h := w.Header()
	for name, values := range forwardable(resp.Header, false) {
		h[name] = values
	}
	w.WriteHeader(resp.StatusCode)
	copyErr := copyFlush(w, resCapture)
	end := time.Now()
	statusLine := fmt.Sprintf("%s %s", resp.Proto, resp.Status)
	res := resCapture.message(resp.Header, statusLine, p.opts.BodyLimit)
	f.Request = reqCapture.message(reqHeader, requestLine, p.opts.BodyLimit)
	f.Response = &res
	f.Status = resp.StatusCode
	f.Protocol = resp.Proto
	f.BytesSent = f.Request.HeaderBytes + f.Request.BodyBytes
	f.BytesReceived = res.HeaderBytes + res.BodyBytes
	f.ConnectMS = tm.connectMS()
	f.TTFBMS = tm.ttfbMS()
	if resp.TLS != nil {
		f.TLS = tlsInfo(resp.TLS, true)
	}
	if copyErr != nil {
		f.Error = "response cut short: " + copyErr.Error()
	}
	return f.Completed(end), copyErr
}

// copyFlush relays r to w, flushing after every write so a stream or a
// slow body reaches the client as it arrives. It returns the read error, if
// any, other than EOF; a failed write to the client is not an upstream fault.
func copyFlush(w http.ResponseWriter, r io.Reader) error {
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 32<<10)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return nil
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func tlsInfo(cs *tls.ConnectionState, intercepted bool) *flow.TLSInfo {
	return &flow.TLSInfo{
		ServerName:  cs.ServerName,
		Version:     tls.VersionName(cs.Version),
		Cipher:      tls.CipherSuiteName(cs.CipherSuite),
		Intercepted: intercepted,
	}
}

// fail answers the client with status and records the error.
func (p *Proxy) fail(w http.ResponseWriter, f flow.Flow, status int, msg string) {
	http.Error(w, msg, status)
	f.Status = status
	f.Error = msg
	p.finish(f.Completed(time.Now()))
}

// finish stores the completed flow. When the first owner lookup found no
// process, it asks again: the machine view may have caught up meanwhile.
func (p *Proxy) finish(f flow.Flow) {
	if f.ClientPID == 0 && p.opts.Owner != nil {
		if pid, name, ok := p.opts.Owner(f.Client); ok {
			f.ClientPID, f.ClientProcess = pid, name
		}
	}
	p.opts.Store.Update(f)
	p.log.Debug("flow", "id", f.ID, "kind", f.Kind, "method", f.Method, "host", f.Host, "status", f.Status, "ms", f.DurationMS)
}
