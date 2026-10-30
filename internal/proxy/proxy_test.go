package proxy

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/nodemode-dev/formnet/internal/ca"
	"github.com/nodemode-dev/formnet/internal/flow"
)

type harness struct {
	proxy    *Proxy
	store    *flow.Store
	root     *x509.CertPool // the formnet root, for clients that let the proxy open TLS
	proxyURL *url.URL
}

func fakeOwner(addr string) (int, string, bool) { return 42, "fakeapp", true }

// start runs a proxy on a free loopback port with a fresh CA.
func start(t *testing.T, opts Options) *harness {
	t.Helper()
	authority, err := ca.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	opts.CA = authority
	if opts.Store == nil {
		opts.Store = flow.NewStore(100)
	}
	if opts.Owner == nil {
		opts.Owner = fakeOwner
	}
	opts.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	p := New(opts)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.ListenAndServe(ctx, "127.0.0.1:0") }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("proxy did not stop")
		}
	})
	deadline := time.Now().Add(3 * time.Second)
	for !p.Status().Running {
		if time.Now().After(deadline) {
			t.Fatalf("proxy never started: %+v", p.Status())
		}
		time.Sleep(5 * time.Millisecond)
	}
	root := x509.NewCertPool()
	root.AppendCertsFromPEM(authority.PEM())
	u, _ := url.Parse("http://" + p.Status().Addr)
	return &harness{proxy: p, store: opts.Store, root: root, proxyURL: u}
}

// client talks through the proxy, trusting roots for TLS it sees.
func (h *harness) client(roots *x509.CertPool) *http.Client {
	tr := &http.Transport{Proxy: http.ProxyURL(h.proxyURL), TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, DisableKeepAlives: true}
	return &http.Client{Transport: tr, Timeout: 10 * time.Second}
}

func poolOf(srv *httptest.Server) *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	return pool
}

// waitFlow polls the store for a flow matching pred.
func waitFlow(t *testing.T, store *flow.Store, pred func(flow.Flow) bool) flow.Flow {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, f := range store.List(flow.Query{}) {
			if full, ok := store.Get(f.ID); ok && pred(full) {
				return full
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no matching flow; have %+v", store.List(flow.Query{}))
	return flow.Flow{}
}

func completed(f flow.Flow) bool { return !f.Pending }

// drain fetches a URL and reads and closes the body, so the connection ends.
func drain(t *testing.T, c *http.Client, target string) {
	t.Helper()
	res, err := c.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(res.Body)
	res.Body.Close()
}

// TestIdleTunnelIsClosed covers the idle watch: the server has finished, the
// client never reads or closes, the tunnel still ends after TunnelIdle.
func TestIdleTunnelIsClosed(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("x")) }))
	defer upstream.Close()
	h := start(t, Options{Intercept: false, TunnelIdle: 300 * time.Millisecond})
	c := h.client(poolOf(upstream))
	if _, err := c.Get(upstream.URL); err != nil { // body neither read nor closed
		t.Fatal(err)
	}
	f := waitFlow(t, h.store, func(f flow.Flow) bool { return f.Kind == flow.KindTunnel && completed(f) })
	if f.BytesReceived == 0 {
		t.Fatalf("tunnel: %+v", f)
	}
}

func TestListenRefusesNonLoopback(t *testing.T) {
	p := New(Options{})
	for _, addr := range []string{"0.0.0.0:0", "192.0.2.1:0", "[::]:0", "nope"} {
		if err := p.ListenAndServe(context.Background(), addr); err == nil {
			t.Errorf("%s accepted", addr)
		}
	}
	if p.Status().Error == "" {
		t.Error("status carries no error")
	}
}

func TestPlainHTTPIsForwardedAndRecorded(t *testing.T) {
	var seen http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("X-Answer", "yes")
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("got " + r.Method + " " + string(body)))
	}))
	defer upstream.Close()
	h := start(t, Options{Intercept: true})
	req, _ := http.NewRequest(http.MethodPost, upstream.URL+"/submit?x=1", strings.NewReader("hello"))
	req.Header.Set("X-Test", "abc")
	req.Header.Set("Proxy-Authorization", "Basic secret")
	req.Header.Set("Proxy-Connection", "keep-alive")
	res, err := h.client(nil).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusCreated || string(body) != "got POST hello" || res.Header.Get("X-Answer") != "yes" {
		t.Fatalf("response: %d %q %v", res.StatusCode, body, res.Header)
	}
	if seen.Get("Proxy-Authorization") != "" || seen.Get("Proxy-Connection") != "" || seen.Get("X-Test") != "abc" {
		t.Fatalf("upstream headers: %v", seen)
	}
	f := waitFlow(t, h.store, completed)
	if f.Kind != flow.KindHTTP || f.Method != "POST" || f.Status != 201 || f.Scheme != "http" || f.Path != "/submit" || f.Query != "x=1" {
		t.Fatalf("flow: %+v", f)
	}
	if f.ClientPID != 42 || f.ClientProcess != "fakeapp" || !strings.HasPrefix(f.Client, "127.0.0.1:") {
		t.Fatalf("client: %+v", f)
	}
	if string(f.Request.Body) != "hello" || f.Request.BodyBytes != 5 || string(f.Response.Body) != "got POST hello" {
		t.Fatalf("bodies: %q %q", f.Request.Body, f.Response.Body)
	}
	if f.Response.ContentType != "text/plain" || f.Protocol != "HTTP/1.1" || f.ClientProtocol != "HTTP/1.1" || f.BytesSent <= 5 || f.BytesReceived <= 14 || f.DurationMS <= 0 {
		t.Fatalf("record: %+v", f)
	}
	if f.TLS != nil {
		t.Fatal("plain http carries no TLS")
	}
	var hasTest bool
	for _, hd := range f.Request.Headers {
		if hd.Name == "X-Test" && hd.Value == "abc" {
			hasTest = true
		}
	}
	if !hasTest {
		t.Fatalf("request headers: %+v", f.Request.Headers)
	}
}

func TestHTTPSIsOpenedWithTheRoot(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"secret":"visible"}`))
	}))
	defer upstream.Close()
	h := start(t, Options{Intercept: true, UpstreamRootCAs: poolOf(upstream)})
	res, err := h.client(h.root).Get(upstream.URL + "/api")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if string(body) != `{"secret":"visible"}` {
		t.Fatalf("body %q", body)
	}
	if len(res.TLS.PeerCertificates) == 0 || res.TLS.PeerCertificates[0].Issuer.Organization[0] != "formnet" {
		t.Fatal("the client did not see a formnet leaf")
	}
	f := waitFlow(t, h.store, func(f flow.Flow) bool { return completed(f) && f.Kind == flow.KindHTTP })
	if f.Scheme != "https" || f.Status != 200 || string(f.Response.Body) != `{"secret":"visible"}` || f.Response.ContentType != "application/json" {
		t.Fatalf("flow: %+v", f)
	}
	if f.TLS == nil || !f.TLS.Intercepted || f.TLS.Version == "" || f.TLS.Cipher == "" {
		t.Fatalf("tls: %+v", f.TLS)
	}
	if f.Protocol == "" || f.ClientProtocol != "HTTP/1.1" || f.ClientProcess != "fakeapp" {
		t.Fatalf("record: %+v", f)
	}
	for _, g := range h.store.List(flow.Query{}) {
		if g.Kind == flow.KindTunnel {
			t.Fatal("an opened CONNECT must not also be a tunnel flow")
		}
	}
}

func TestGzipBodyIsDecodedWhenWhole(t *testing.T) {
	var zipped bytes.Buffer
	zw := gzip.NewWriter(&zipped)
	_, _ = zw.Write([]byte("plain text inside gzip"))
	zw.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write(zipped.Bytes())
	}))
	defer upstream.Close()
	h := start(t, Options{Intercept: true})
	req, _ := http.NewRequest(http.MethodGet, upstream.URL, nil)
	req.Header.Set("Accept-Encoding", "gzip")
	res, err := h.client(nil).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if !bytes.Equal(raw, zipped.Bytes()) {
		t.Fatal("the client must receive the bytes as sent")
	}
	f := waitFlow(t, h.store, completed)
	if string(f.Response.Body) != "plain text inside gzip" || f.Response.Encoding != "gzip" || f.Response.BodyBytes != int64(zipped.Len()) || f.Response.BodyTruncated {
		t.Fatalf("response: %+v %q", f.Response, f.Response.Body)
	}
}

func TestBodyLimitTruncates(t *testing.T) {
	payload := strings.Repeat("x", 1000)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(payload))
	}))
	defer upstream.Close()
	h := start(t, Options{Intercept: true, BodyLimit: 10})
	res, err := h.client(nil).Post(upstream.URL, "text/plain", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if len(got) != 1000 {
		t.Fatalf("client got %d bytes", len(got))
	}
	f := waitFlow(t, h.store, completed)
	if len(f.Response.Body) != 10 || !f.Response.BodyTruncated || f.Response.BodyBytes != 1000 {
		t.Fatalf("response: %+v", f.Response)
	}
	if len(f.Request.Body) != 10 || !f.Request.BodyTruncated || f.Request.BodyBytes != 1000 {
		t.Fatalf("request: %+v", f.Request)
	}
}

func TestPassthroughAndInterceptOffAreTunnels(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("pinned"))
	}))
	defer upstream.Close()
	h := start(t, Options{Intercept: true, Passthrough: []string{"127.0.0.1"}, UpstreamRootCAs: poolOf(upstream)})

	// Passthrough: the client sees the upstream's own certificate.
	res, err := h.client(poolOf(upstream)).Get(upstream.URL + "/pinned")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if string(body) != "pinned" {
		t.Fatalf("body %q", body)
	}
	f := waitFlow(t, h.store, func(f flow.Flow) bool { return f.Kind == flow.KindTunnel && completed(f) })
	if f.Method != http.MethodConnect || f.BytesSent == 0 || f.BytesReceived == 0 || f.Response != nil || len(f.Request.Body) != 0 {
		t.Fatalf("tunnel: %+v", f)
	}
	if f.TLS == nil || f.TLS.Intercepted || f.TLS.ServerName != "127.0.0.1" {
		t.Fatalf("tunnel tls: %+v", f.TLS)
	}

	// Clearing the passthrough list makes the next connection opened.
	h.proxy.SetPassthrough(nil)
	h.store.Clear()
	drain(t, h.client(h.root), upstream.URL+"/open")
	waitFlow(t, h.store, func(f flow.Flow) bool { return f.Kind == flow.KindHTTP && f.Path == "/open" && completed(f) })

	// Interception off: a tunnel again, with the upstream's certificate.
	h.proxy.SetIntercept(false)
	h.store.Clear()
	drain(t, h.client(poolOf(upstream)), upstream.URL+"/off")
	waitFlow(t, h.store, func(f flow.Flow) bool { return f.Kind == flow.KindTunnel && completed(f) })
	if st := h.proxy.Status(); st.Intercept || len(st.Passthrough) != 0 || !st.Running {
		t.Fatalf("status: %+v", st)
	}
}

func TestUpstreamFailuresAre502(t *testing.T) {
	closed, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := closed.Addr().String()
	closed.Close()
	h := start(t, Options{Intercept: true})
	res, err := h.client(nil).Get("http://" + addr + "/x")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadGateway {
		t.Fatalf("plain: %d", res.StatusCode)
	}
	f := waitFlow(t, h.store, completed)
	if f.Error == "" || f.Status != 502 {
		t.Fatalf("flow: %+v", f)
	}
	// Opened CONNECT: the dial happens on the inner request, which gets the 502.
	h.store.Clear()
	res, err = h.client(h.root).Get("https://" + addr + "/y")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadGateway {
		t.Fatalf("inner request: %d", res.StatusCode)
	}
	g := waitFlow(t, h.store, func(f flow.Flow) bool { return f.Kind == flow.KindHTTP && completed(f) })
	if g.Error == "" || g.Status != 502 || g.Scheme != "https" {
		t.Fatalf("inner flow: %+v", g)
	}
	// Tunnelled CONNECT: the dial happens first and the CONNECT itself is refused.
	h.proxy.SetPassthrough([]string{"127.0.0.1"})
	h.store.Clear()
	if _, err := h.client(nil).Get("https://" + addr + "/z"); err == nil || !strings.Contains(err.Error(), "Bad Gateway") {
		t.Fatalf("connect to a closed port: %v", err)
	}
	tf := waitFlow(t, h.store, func(f flow.Flow) bool { return f.Kind == flow.KindTunnel && completed(f) })
	if tf.Error == "" || tf.Status != 502 {
		t.Fatalf("tunnel flow: %+v", tf)
	}
}

func wsAccept(key string) string {
	sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	return base64.StdEncoding.EncodeToString(sum[:])
}

// echoServer answers a WebSocket handshake and echoes whatever bytes follow.
func echoServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			http.Error(w, "not an upgrade", http.StatusBadRequest)
			return
		}
		conn, bufrw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_, _ = io.WriteString(conn, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: "+wsAccept(r.Header.Get("Sec-WebSocket-Key"))+"\r\n\r\n")
		buf := make([]byte, 64)
		for {
			n, err := bufrw.Read(buf)
			if n > 0 {
				_, _ = conn.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}))
}

func TestWebSocketIsRelayedAndCounted(t *testing.T) {
	upstream := echoServer(t)
	defer upstream.Close()
	h := start(t, Options{Intercept: true})
	conn, err := net.Dial("tcp", h.proxyURL.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, _ = io.WriteString(conn, "GET "+upstream.URL+"/ws HTTP/1.1\r\nHost: "+strings.TrimPrefix(upstream.URL, "http://")+"\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n")
	reader := bufio.NewReader(conn)
	res, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusSwitchingProtocols || res.Header.Get("Sec-WebSocket-Accept") != wsAccept("dGhlIHNhbXBsZSBub25jZQ==") {
		t.Fatalf("handshake: %d %v", res.StatusCode, res.Header)
	}
	f := waitFlow(t, h.store, func(f flow.Flow) bool { return f.Kind == flow.KindWebSocket && f.Status == 101 })
	if f.Pending != true || f.Path != "/ws" || f.ClientProtocol != "HTTP/1.1" {
		t.Fatalf("flow before data: %+v", f)
	}
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write([]byte("frame-bytes")); err != nil {
		t.Fatal(err)
	}
	echo := make([]byte, len("frame-bytes"))
	if _, err := io.ReadFull(reader, echo); err != nil || string(echo) != "frame-bytes" {
		t.Fatalf("echo %q %v", echo, err)
	}
	conn.Close()
	done := waitFlow(t, h.store, func(f flow.Flow) bool { return f.Kind == flow.KindWebSocket && completed(f) })
	if done.BytesSent < done.Request.HeaderBytes+11 || done.BytesReceived < done.Response.HeaderBytes+11 {
		t.Fatalf("counts: sent %d received %d", done.BytesSent, done.BytesReceived)
	}
	if done.Response.Headers == nil || done.Request.Headers == nil {
		t.Fatal("handshake headers not recorded")
	}
}

func TestWebSocketInsideAnOpenedTunnel(t *testing.T) {
	upstream := httptest.NewTLSServer(echoServer(t).Config.Handler)
	defer upstream.Close()
	h := start(t, Options{Intercept: true, UpstreamRootCAs: poolOf(upstream)})
	raw, err := net.Dial("tcp", h.proxyURL.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	hostPort := strings.TrimPrefix(upstream.URL, "https://")
	_, _ = io.WriteString(raw, "CONNECT "+hostPort+" HTTP/1.1\r\nHost: "+hostPort+"\r\n\r\n")
	br := bufio.NewReader(raw)
	res, err := http.ReadResponse(br, nil)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("connect: %v %v", err, res)
	}
	conn := tls.Client(raw, &tls.Config{ServerName: "127.0.0.1", RootCAs: h.root, MinVersion: tls.VersionTLS12})
	if err := conn.Handshake(); err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(conn, "GET /ws HTTP/1.1\r\nHost: "+hostPort+"\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n")
	reader := bufio.NewReader(conn)
	ures, err := http.ReadResponse(reader, nil)
	if err != nil || ures.StatusCode != 101 {
		t.Fatalf("upgrade: %v %v", err, ures)
	}
	_, _ = conn.Write([]byte("tls-frame"))
	echo := make([]byte, 9)
	_ = raw.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.ReadFull(reader, echo); err != nil || string(echo) != "tls-frame" {
		t.Fatalf("echo %q %v", echo, err)
	}
	f := waitFlow(t, h.store, func(f flow.Flow) bool { return f.Kind == flow.KindWebSocket && f.Status == 101 })
	if f.Scheme != "https" || f.TLS == nil || !f.TLS.Intercepted {
		t.Fatalf("flow: %+v", f)
	}
}

func TestPlainHTTPInsideConnect(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("inside"))
	}))
	defer upstream.Close()
	h := start(t, Options{Intercept: true})
	raw, err := net.Dial("tcp", h.proxyURL.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	hostPort := strings.TrimPrefix(upstream.URL, "http://")
	_, _ = io.WriteString(raw, "CONNECT "+hostPort+" HTTP/1.1\r\nHost: "+hostPort+"\r\n\r\n")
	br := bufio.NewReader(raw)
	if res, err := http.ReadResponse(br, nil); err != nil || res.StatusCode != 200 {
		t.Fatalf("connect: %v %v", err, res)
	}
	_, _ = io.WriteString(raw, "GET /plain HTTP/1.1\r\nHost: "+hostPort+"\r\n\r\n")
	res, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	if string(body) != "inside" {
		t.Fatalf("body %q", body)
	}
	f := waitFlow(t, h.store, func(f flow.Flow) bool { return f.Kind == flow.KindHTTP && completed(f) })
	if f.Scheme != "http" || f.Path != "/plain" || f.TLS != nil {
		t.Fatalf("flow: %+v", f)
	}
}

func TestServerNameParsesAClientHello(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 16384)
		n, _ := io.ReadAtLeast(conn, buf, 5)
		if n >= 5 {
			want := 5 + (int(buf[3])<<8 | int(buf[4]))
			for n < want {
				m, err := conn.Read(buf[n:])
				n += m
				if err != nil {
					break
				}
			}
		}
		got <- serverName(buf[:n])
	}()
	conn := tls.Client(mustDial(t, ln.Addr().String()), &tls.Config{ServerName: "sni.example.test", InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	_ = conn.Handshake()
	conn.Close()
	select {
	case name := <-got:
		if name != "sni.example.test" {
			t.Fatalf("sni %q", name)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no hello")
	}
	if serverName(nil) != "" || serverName([]byte{0x16, 0x03, 0x01, 0x00, 0x05, 0x01, 0, 0, 0, 0}) != "" {
		t.Fatal("garbage must give an empty name")
	}
}

func mustDial(t *testing.T, addr string) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestForwardableStripsHopByHop(t *testing.T) {
	h := http.Header{}
	h.Set("Connection", "close, X-Custom")
	h.Set("X-Custom", "1")
	h.Set("Keep-Alive", "timeout=5")
	h.Set("Proxy-Authorization", "x")
	h.Set("Transfer-Encoding", "chunked")
	h.Set("Upgrade", "websocket")
	h.Set("Accept-Encoding", "gzip")
	out := forwardable(h, false)
	for _, gone := range []string{"Connection", "X-Custom", "Keep-Alive", "Proxy-Authorization", "Transfer-Encoding", "Upgrade"} {
		if out.Get(gone) != "" {
			t.Errorf("%s forwarded", gone)
		}
	}
	if out.Get("Accept-Encoding") != "gzip" {
		t.Error("Accept-Encoding must be kept as sent")
	}
	kept := forwardable(h, true)
	if kept.Get("Upgrade") != "websocket" || kept.Get("Connection") == "" {
		t.Error("an upgrade keeps Connection and Upgrade")
	}
}
