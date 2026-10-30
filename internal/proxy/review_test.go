package proxy

import (
	"bufio"
	"bytes"
	"compress/flate"
	"compress/zlib"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nodemode-dev/formnet/internal/flow"
)

// fakeResolver answers from a table; unknown names fail.
type fakeResolver map[string][]string

func (r fakeResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	ips, ok := r[strings.ToLower(host)]
	if !ok {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	out := make([]net.IPAddr, 0, len(ips))
	for _, ip := range ips {
		out = append(out, net.IPAddr{IP: net.ParseIP(ip)})
	}
	return out, nil
}

func TestGuardResolveRules(t *testing.T) {
	g := newGuard([]string{"127.0.0.1:8760", "localhost:8761", "[::1]:8760", "bad"}, fakeResolver{
		"rebind.test":   {"93.184.216.34", "127.0.0.1"},
		"rebind6.test":  {"::1"},
		"zero.test":     {"0.0.0.0"},
		"intranet.test": {"10.1.2.3"},
		"public.test":   {"93.184.216.34"},
	})
	allowed := []string{"93.184.216.34:443", "10.0.0.5:80", "127.0.0.1:9000", "localhost:9000", "app.localhost:3000", "[::1]:9000", "intranet.test:443", "public.test:443"}
	for _, target := range allowed {
		if _, err := g.resolve(context.Background(), target); err != nil {
			t.Errorf("%s refused: %v", target, err)
		}
	}
	refused := map[string]string{
		"127.0.0.1:8760":  "own address",
		"localhost:8760":  "own address",
		"0.0.0.0:8760":    "own address",
		"[::1]:8760":      "own address",
		"[::]:8760":       "own address",
		"127.0.0.1:8761":  "own address",
		"rebind.test:80":  "loopback",
		"rebind6.test:80": "loopback",
		"zero.test:80":    "loopback",
		"nohost":          "host:port",
	}
	for target, why := range refused {
		_, err := g.resolve(context.Background(), target)
		var re *RefusedError
		if err == nil || !errorsAs(err, &re) || !strings.Contains(err.Error(), why) {
			t.Errorf("%s: want refusal about %q, got %v", target, why, err)
		}
	}
	if _, err := g.resolve(context.Background(), "unknown.test:80"); err == nil || statusFor(err) != 502 {
		t.Errorf("unknown name must be a plain failure, got %v", err)
	}
	if _, err := g.DialContext(context.Background(), "udp", "93.184.216.34:53"); err == nil || statusFor(err) != 403 {
		t.Errorf("udp must be refused, got %v", err)
	}
}

func errorsAs(err error, target **RefusedError) bool {
	for err != nil {
		if re, ok := err.(*RefusedError); ok {
			*target = re
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func TestProxyRefusesItsOwnAddressesAndRebinding(t *testing.T) {
	// A local service standing in for formnet's own API.
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("api")) }))
	defer local.Close()
	localAddr := strings.TrimPrefix(local.URL, "http://")
	_, port, _ := net.SplitHostPort(localAddr)
	h := start(t, Options{Intercept: true, Refuse: []string{localAddr}, Resolver: fakeResolver{"rebind.test": {"127.0.0.1"}, "localhost": {"127.0.0.1"}}})
	c := h.client(h.root)

	// Plain request to the refused address.
	res, err := c.Get(local.URL + "/api/flows")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("own address: %d", res.StatusCode)
	}
	f := waitFlow(t, h.store, completed)
	if f.Status != 403 || !strings.Contains(f.Error, "own address") {
		t.Fatalf("flow: %+v", f)
	}

	// CONNECT to the refused address, in intercept mode and in tunnel mode.
	for _, intercept := range []bool{true, false} {
		h.proxy.SetIntercept(intercept)
		h.store.Clear()
		_, err = c.Get("https://" + localAddr + "/x")
		if err == nil || !strings.Contains(err.Error(), "Forbidden") {
			t.Fatalf("connect (intercept=%v): %v", intercept, err)
		}
		tf := waitFlow(t, h.store, func(f flow.Flow) bool { return f.Kind == flow.KindTunnel && completed(f) })
		if tf.Status != 403 || tf.Error == "" {
			t.Fatalf("tunnel flow: %+v", tf)
		}
	}
	h.proxy.SetIntercept(true)

	// A name that resolves to loopback is the rebinding pivot.
	h.store.Clear()
	res, err = c.Get("http://rebind.test:" + port + "/")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("rebinding name: %d", res.StatusCode)
	}
	rf := waitFlow(t, h.store, completed)
	if !strings.Contains(rf.Error, "loopback") {
		t.Fatalf("rebinding flow: %+v", rf)
	}

	// WebSocket upgrade to the refused address.
	h.store.Clear()
	req, _ := http.NewRequest(http.MethodGet, local.URL+"/ws", nil)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	req.Header.Set("Sec-WebSocket-Version", "13")
	res, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("websocket: %d", res.StatusCode)
	}
	wf := waitFlow(t, h.store, func(f flow.Flow) bool { return f.Kind == flow.KindWebSocket && completed(f) })
	if wf.Status != 403 {
		t.Fatalf("websocket flow: %+v", wf)
	}

	// localhost by name on another port is a legitimate local dev server.
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("dev")) }))
	defer other.Close()
	_, otherPort, _ := net.SplitHostPort(strings.TrimPrefix(other.URL, "http://"))
	res, err = c.Get("http://localhost:" + otherPort + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || string(body) != "dev" {
		t.Fatalf("localhost dev server: %d %q", res.StatusCode, body)
	}
}

func TestStreamedBodyArrivesAsItIsSent(t *testing.T) {
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: first\n\n")
		w.(http.Flusher).Flush()
		<-release
		_, _ = io.WriteString(w, "data: second\n\n")
	}))
	defer upstream.Close()
	defer close(release)
	h := start(t, Options{Intercept: true})
	res, err := h.client(nil).Get(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	first := make([]byte, len("data: first\n\n"))
	done := make(chan error, 1)
	go func() { _, err := io.ReadFull(res.Body, first); done <- err }()
	select {
	case err := <-done:
		if err != nil || string(first) != "data: first\n\n" {
			t.Fatalf("first chunk: %q %v", first, err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the first chunk was held back until the second")
	}
}

func TestTruncatedUpstreamResetsTheClient(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, bufrw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		_, _ = bufrw.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\nonly ten!!")
		_ = bufrw.Flush()
		conn.Close()
	}))
	defer upstream.Close()
	h := start(t, Options{Intercept: true})
	res, err := h.client(nil).Get(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(res.Body)
	res.Body.Close()
	if readErr == nil {
		t.Fatalf("the client must not see a clean end after %d bytes", len(body))
	}
	f := waitFlow(t, h.store, completed)
	if !strings.Contains(f.Error, "cut short") || f.Response.BodyBytes != 10 {
		t.Fatalf("flow: %+v", f)
	}
}

func TestProxyAddsNoAcceptEncoding(t *testing.T) {
	var seen http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen = r.Header.Clone() }))
	defer upstream.Close()
	h := start(t, Options{Intercept: true})
	tr := &http.Transport{Proxy: http.ProxyURL(h.proxyURL), DisableCompression: true, DisableKeepAlives: true}
	res, err := (&http.Client{Transport: tr}).Get(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if _, has := seen["Accept-Encoding"]; has {
		t.Fatalf("proxy added Accept-Encoding: %v", seen)
	}
}

func TestHalfCloseIsHonoured(t *testing.T) {
	// A server that answers only after the client has finished sending.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				all, _ := io.ReadAll(conn)
				fmt.Fprintf(conn, "got %d bytes", len(all))
			}()
		}
	}()
	h := start(t, Options{Intercept: false})
	raw, err := net.Dial("tcp", h.proxyURL.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	_, _ = io.WriteString(raw, "CONNECT "+ln.Addr().String()+" HTTP/1.1\r\nHost: "+ln.Addr().String()+"\r\n\r\n")
	br := bufio.NewReader(raw)
	if res, err := http.ReadResponse(br, nil); err != nil || res.StatusCode != 200 {
		t.Fatalf("connect: %v %v", err, res)
	}
	_, _ = raw.Write([]byte("hello"))
	_ = raw.(*net.TCPConn).CloseWrite()
	_ = raw.SetReadDeadline(time.Now().Add(3 * time.Second))
	answer, _ := io.ReadAll(br)
	if string(answer) != "got 5 bytes" {
		t.Fatalf("answer after half-close: %q", answer)
	}
	f := waitFlow(t, h.store, func(f flow.Flow) bool { return f.Kind == flow.KindTunnel && completed(f) })
	if f.BytesSent != 5 || f.BytesReceived != 11 || f.TLS != nil {
		t.Fatalf("plain tunnel flow: %+v", f)
	}
}

func TestDeflateBothShapes(t *testing.T) {
	var zl bytes.Buffer
	zw := zlib.NewWriter(&zl)
	_, _ = zw.Write([]byte("zlib wrapped"))
	zw.Close()
	var raw bytes.Buffer
	fw, _ := flate.NewWriter(&raw, flate.DefaultCompression)
	_, _ = fw.Write([]byte("raw deflate"))
	fw.Close()
	if d, ok := decode(zl.Bytes(), "deflate", 1024); !ok || string(d.body) != "zlib wrapped" {
		t.Fatalf("zlib: %v %q", ok, d.body)
	}
	if d, ok := decode(raw.Bytes(), "deflate", 1024); !ok || string(d.body) != "raw deflate" {
		t.Fatalf("raw: %v %q", ok, d.body)
	}
	if d, ok := decode(zl.Bytes(), "deflate", 4); !ok || !d.truncated || len(d.body) != 4 {
		t.Fatalf("limit: %v %+v", ok, d)
	}
	if _, ok := decode([]byte("not compressed"), "gzip", 1024); ok {
		t.Fatal("garbage decoded")
	}
}

// clientHello builds a minimal ClientHello record with the given extensions in order.
func clientHello(exts ...[]byte) []byte {
	var extBody []byte
	for _, e := range exts {
		extBody = append(extBody, e...)
	}
	body := []byte{0x03, 0x03}
	body = append(body, make([]byte, 32)...) // random
	body = append(body, 0)                   // session id length
	body = append(body, 0x00, 0x02, 0x13, 0x01)
	body = append(body, 0x01, 0x00) // compression
	body = append(body, byte(len(extBody)>>8), byte(len(extBody)))
	body = append(body, extBody...)
	hs := append([]byte{0x01, byte(len(body) >> 16), byte(len(body) >> 8), byte(len(body))}, body...)
	return append([]byte{0x16, 0x03, 0x01, byte(len(hs) >> 8), byte(len(hs))}, hs...)
}

func sniExt(name string) []byte {
	list := append([]byte{0x00, byte(len(name) >> 8), byte(len(name))}, name...)
	list = append([]byte{byte(len(list) >> 8), byte(len(list))}, list...)
	return append([]byte{0x00, 0x00, byte(len(list) >> 8), byte(len(list))}, list...)
}

func paddingExt(n int) []byte {
	return append([]byte{0x00, 0x15, byte(n >> 8), byte(n)}, make([]byte, n)...)
}

func TestServerNameWithSNILast(t *testing.T) {
	if got := serverName(clientHello(paddingExt(40), sniExt("last.example"))); got != "last.example" {
		t.Fatalf("sni last: %q", got)
	}
	if got := serverName(clientHello(sniExt("first.example"), paddingExt(10))); got != "first.example" {
		t.Fatalf("sni first: %q", got)
	}
	if got := serverName(clientHello(paddingExt(10))); got != "" {
		t.Fatalf("no sni: %q", got)
	}
	rec := clientHello(sniExt("x.example"))
	if got := serverName(rec[:len(rec)-3]); got != "" {
		t.Fatalf("cut record must not panic or guess: %q", got)
	}
}

func TestPeekRecordIgnoresNonTLS(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	go func() { _, _ = client.Write([]byte("GET / HTTP/1.1\r\n")) }()
	pc := newPeekedConn(server, nil)
	if rec := pc.peekRecord(time.Second); rec != nil {
		t.Fatalf("plain bytes gave a record: %q", rec)
	}
	first, err := pc.peek(3, time.Second)
	if err != nil || string(first) != "GET" {
		t.Fatalf("bytes must still be there: %q %v", first, err)
	}
}
