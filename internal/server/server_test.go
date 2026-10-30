package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/nodemode-dev/formnet/internal/ca"
	"github.com/nodemode-dev/formnet/internal/flow"
	"github.com/nodemode-dev/formnet/internal/machine"
	"github.com/nodemode-dev/formnet/internal/proxy"
)

type fakeProxy struct {
	status proxy.Status
}

func (p *fakeProxy) Status() proxy.Status             { return p.status }
func (p *fakeProxy) SetIntercept(on bool)             { p.status.Intercept = on }
func (p *fakeProxy) SetPassthrough(patterns []string) { p.status.Passthrough = patterns }

type fakeCA struct{}

func (fakeCA) Info() ca.Info {
	return ca.Info{Fingerprint: "AA:BB", Trusted: false, TrustCommand: "security add-trusted-cert"}
}
func (fakeCA) PEM() []byte {
	return []byte("-----BEGIN CERTIFICATE-----\nMA==\n-----END CERTIFICATE-----\n")
}

type fakeMachine struct {
	ch chan machine.Snapshot
}

func (m *fakeMachine) Latest() machine.Snapshot {
	return machine.Snapshot{Supported: true, Source: "test", Processes: []machine.Process{{PID: 1, Name: "init"}}}
}
func (m *fakeMachine) Subscribe(ctx context.Context) <-chan machine.Snapshot { return m.ch }

const testToken = "0123456789abcdef0123456789abcdef"

func newTest(t *testing.T) (*httptest.Server, *flow.Store, *fakeProxy) {
	t.Helper()
	store := flow.NewStore(100)
	p := &fakeProxy{status: proxy.Status{Addr: "127.0.0.1:8761", Running: true, Intercept: true}}
	dist := fstest.MapFS{
		"index.html":    {Data: []byte("<!doctype html><title>formnet</title>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
		"favicon.svg":   {Data: []byte("<svg/>")},
	}
	h := Handler(Deps{Addr: "127.0.0.1:8760", Token: testToken, Store: store, Proxy: p, CA: fakeCA{}, Machine: &fakeMachine{ch: make(chan machine.Snapshot, 1)}, Dist: dist})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, store, p
}

// do sends a request with the Host header the guard expects.
func do(t *testing.T, srv *httptest.Server, method, path string, body string, headers map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "127.0.0.1:8760"
	req.Header.Set("Authorization", "Bearer "+testToken)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func decode(t *testing.T, res *http.Response) map[string]any {
	t.Helper()
	defer res.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestHostGuardRefusesOtherHosts(t *testing.T) {
	srv, _, _ := newTest(t)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/healthz", nil)
	req.Host = "evil.example:8760"
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("rebinding host got %d", res.StatusCode)
	}
	req.Host = "127.0.0.1:9999"
	if res, _ = srv.Client().Do(req); res.StatusCode != http.StatusForbidden {
		t.Fatalf("other port got %d", res.StatusCode)
	}
	for _, ok := range []string{"127.0.0.1:8760", "localhost:8760", "[::1]:8760"} {
		req.Host = ok
		if res, _ = srv.Client().Do(req); res.StatusCode != http.StatusOK {
			t.Fatalf("host %q got %d", ok, res.StatusCode)
		}
	}
}

func TestCrossSiteWritesRefused(t *testing.T) {
	srv, store, _ := newTest(t)
	store.Add(flow.Flow{ID: "1", Kind: flow.KindHTTP, Host: "a.example"})
	res := do(t, srv, http.MethodPost, "/api/flows/clear", "", map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "http://evil.example"})
	if res.StatusCode != http.StatusForbidden || store.Len() != 1 {
		t.Fatalf("cross-site clear: %d, len %d", res.StatusCode, store.Len())
	}
	res = do(t, srv, http.MethodPost, "/api/flows/clear", "", map[string]string{"Origin": "http://evil.example"})
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign origin clear: %d", res.StatusCode)
	}
	res = do(t, srv, http.MethodPost, "/api/flows/clear", "", map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "http://127.0.0.1:8760"})
	if res.StatusCode != http.StatusOK || store.Len() != 0 {
		t.Fatalf("same-origin clear: %d, len %d", res.StatusCode, store.Len())
	}
}

func TestFlowsListDetailAndHAR(t *testing.T) {
	srv, store, _ := newTest(t)
	f := flow.Flow{ID: "f1", Kind: flow.KindHTTP, Start: time.Now(), Method: "POST", Status: 200, Protocol: "HTTP/1.1",
		Request:  flow.Message{Body: []byte(`{"a":1}`), BodyBytes: 7, ContentType: "application/json"},
		Response: &flow.Message{Body: []byte{0x00, 0xff}, BodyBytes: 2, BodyTruncated: true},
	}.WithURL("https://api.example.com/v1?x=1")
	store.Add(f)
	store.Add(flow.Flow{ID: "t1", Kind: flow.KindTunnel, Start: time.Now(), Host: "pinned.example", BytesSent: 10})

	list := decode(t, do(t, srv, http.MethodGet, "/api/flows?host=*.example.com", "", nil))
	flows := list["flows"].([]any)
	if len(flows) != 1 || list["total"].(float64) != 2 {
		t.Fatalf("list: %v", list)
	}
	if _, has := flows[0].(map[string]any)["request_body"]; has {
		t.Fatal("list must not carry bodies")
	}
	if res := do(t, srv, http.MethodGet, "/api/flows?since=abc", "", nil); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad since: %d", res.StatusCode)
	}

	detail := decode(t, do(t, srv, http.MethodGet, "/api/flows/f1", "", nil))
	rb := detail["request_body"].(map[string]any)
	sb := detail["response_body"].(map[string]any)
	if rb["text"] != `{"a":1}` || sb["base64"] != "AP8=" || sb["truncated"] != true {
		t.Fatalf("bodies: %v %v", rb, sb)
	}
	if res := do(t, srv, http.MethodGet, "/api/flows/missing", "", nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("missing flow: %d", res.StatusCode)
	}

	res := do(t, srv, http.MethodGet, "/api/export/har", "", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(res.Header.Get("Content-Disposition"), ".har") {
		t.Fatalf("har: %d %q", res.StatusCode, res.Header.Get("Content-Disposition"))
	}
	har := decode(t, res)
	entries := har["log"].(map[string]any)["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("har entries: %d", len(entries))
	}
	if entries[0].(map[string]any)["request"].(map[string]any)["postData"].(map[string]any)["text"] != `{"a":1}` {
		t.Fatal("har export must carry the bodies the list strips")
	}
}

func TestStatusHostsMachineCAAndProxy(t *testing.T) {
	srv, store, p := newTest(t)
	store.Add(flow.Flow{ID: "1", Kind: flow.KindHTTP, Host: "www.googletagmanager.com", Pending: true, BytesReceived: 10})
	st := decode(t, do(t, srv, http.MethodGet, "/api/status", "", nil))
	if st["flows"].(float64) != 1 || st["pending"].(float64) != 1 || st["hosts"].(float64) != 1 {
		t.Fatalf("status: %v", st)
	}
	if st["machine"].(map[string]any)["processes"].(float64) != 1 || st["ca"].(map[string]any)["fingerprint"] != "AA:BB" {
		t.Fatalf("status parts: %v", st)
	}
	hosts := decode(t, do(t, srv, http.MethodGet, "/api/hosts", "", nil))["hosts"].([]any)
	if hosts[0].(map[string]any)["category"] != "tag manager" {
		t.Fatalf("hosts: %v", hosts)
	}
	res := do(t, srv, http.MethodGet, "/ca.pem", "", nil)
	pem, _ := io.ReadAll(res.Body)
	if res.Header.Get("Content-Type") != "application/x-pem-file" || !strings.HasPrefix(string(pem), "-----BEGIN CERTIFICATE-----") {
		t.Fatalf("ca.pem: %q", pem)
	}
	upd := decode(t, do(t, srv, http.MethodPost, "/api/proxy", `{"intercept":false,"passthrough":[" *.Pinned.example ",""]}`, map[string]string{"Sec-Fetch-Site": "same-origin"}))
	if upd["intercept"] != false || p.status.Passthrough[0] != "*.pinned.example" || len(p.status.Passthrough) != 1 {
		t.Fatalf("proxy update: %v %v", upd, p.status.Passthrough)
	}
	if res := do(t, srv, http.MethodPost, "/api/proxy", `{"passthrough":["bad host/with path"]}`, map[string]string{"Sec-Fetch-Site": "same-origin"}); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad passthrough: %d", res.StatusCode)
	}
	if res := do(t, srv, http.MethodPost, "/api/proxy", `nope`, map[string]string{"Sec-Fetch-Site": "same-origin"}); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad json: %d", res.StatusCode)
	}
}

func TestStreamDeliversFlowEvents(t *testing.T) {
	srv, store, _ := newTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/stream", nil)
	req.Host = "127.0.0.1:8760"
	req.Header.Set("Authorization", "Bearer "+testToken)
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	time.Sleep(50 * time.Millisecond)
	store.Add(flow.Flow{ID: "s1", Kind: flow.KindHTTP, Host: "a.example"})
	buf := make([]byte, 4096)
	var got string
	for !strings.Contains(got, "event: flow.added") {
		n, err := res.Body.Read(buf)
		got += string(buf[:n])
		if err != nil {
			t.Fatalf("stream ended: %v\n%s", err, got)
		}
	}
	if !strings.Contains(got, "event: hello") || !strings.Contains(got, `"epoch":"`) || !strings.Contains(got, `"id":"s1"`) {
		t.Fatalf("stream:\n%s", got)
	}
}

func TestClientServesIndexAssetsAndFallback(t *testing.T) {
	srv, _, _ := newTest(t)
	for _, p := range []string{"/", "/hosts", "/flows/abc"} {
		res := do(t, srv, http.MethodGet, p, "", nil)
		body, _ := io.ReadAll(res.Body)
		if res.StatusCode != http.StatusOK || !strings.Contains(string(body), "<title>formnet</title>") {
			t.Fatalf("%s: %d %q", p, res.StatusCode, body)
		}
		if res.Header.Get("Content-Security-Policy") == "" {
			t.Fatalf("%s: no CSP", p)
		}
	}
	res := do(t, srv, http.MethodGet, "/assets/app.js", "", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(res.Header.Get("Cache-Control"), "immutable") {
		t.Fatalf("asset: %d %q", res.StatusCode, res.Header.Get("Cache-Control"))
	}
	if res := do(t, srv, http.MethodGet, "/api/nothing", "", nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown api route: %d", res.StatusCode)
	}
}

// bare sends a request with the right Host but no session.
func bare(t *testing.T, srv *httptest.Server, method, path string, headers map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "127.0.0.1:8760"
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestSessionRequiredForAPIAndCertificate(t *testing.T) {
	srv, _, _ := newTest(t)
	for _, p := range []string{"/api/status", "/api/flows", "/api/stream", "/ca.pem", "/api/export/har"} {
		res := bare(t, srv, http.MethodGet, p, nil)
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s without a session: %d", p, res.StatusCode)
		}
	}
	if res := bare(t, srv, http.MethodGet, "/", nil); res.StatusCode != http.StatusOK {
		t.Fatalf("the page itself is static and needs no session: %d", res.StatusCode)
	}
	stream := bare(t, srv, http.MethodGet, "/api/stream?token="+testToken, nil)
	stream.Body.Close() // an open stream would keep the test server from closing
	if stream.StatusCode != http.StatusOK {
		t.Fatalf("the stream takes the token as a query parameter: %d", stream.StatusCode)
	}
	if res := bare(t, srv, http.MethodGet, "/ca.pem?token="+testToken, nil); res.StatusCode != http.StatusOK {
		t.Fatalf("downloads take the token as a query parameter: %d", res.StatusCode)
	}
	if res := bare(t, srv, http.MethodGet, "/assets/app.js", nil); res.StatusCode != http.StatusOK {
		t.Fatalf("static files need no session: %d", res.StatusCode)
	}
	if res := bare(t, srv, http.MethodGet, "/healthz", nil); res.StatusCode != http.StatusOK {
		t.Fatalf("healthz needs no session: %d", res.StatusCode)
	}
	if res := bare(t, srv, http.MethodGet, "/api/status", map[string]string{"Authorization": "Bearer " + testToken}); res.StatusCode != http.StatusOK {
		t.Fatalf("bearer token: %d", res.StatusCode)
	}
	if res := bare(t, srv, http.MethodGet, "/api/status", map[string]string{"Authorization": "Bearer wrong"}); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong bearer: %d", res.StatusCode)
	}
}

func TestPageWithTokenSetsNoCookie(t *testing.T) {
	srv, _, _ := newTest(t)
	res := bare(t, srv, http.MethodGet, "/?token="+testToken, nil)
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), "<title>formnet</title>") {
		t.Fatalf("page with token: %d %q", res.StatusCode, body)
	}
	if len(res.Cookies()) != 0 || res.Header.Get("Set-Cookie") != "" {
		t.Fatal("no cookie may be set: a cookie for 127.0.0.1 would reach every other local port")
	}
	if res.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("a token-bearing URL must not leak through the referrer")
	}
	if res := bare(t, srv, http.MethodGet, "/?token=wrong0000000000000000", nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong token: %d", res.StatusCode)
	}
	if res := bare(t, srv, http.MethodGet, "/api/status?token=wrong0000000000000000", nil); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong query token on the API: %d", res.StatusCode)
	}
}

func TestCrossSiteReadsRefused(t *testing.T) {
	srv, _, _ := newTest(t)
	for _, p := range []string{"/api/export/har", "/ca.pem", "/api/flows"} {
		res := do(t, srv, http.MethodGet, p, "", map[string]string{"Sec-Fetch-Site": "cross-site"})
		if res.StatusCode != http.StatusForbidden {
			t.Fatalf("cross-site %s: %d", p, res.StatusCode)
		}
	}
	if res := do(t, srv, http.MethodGet, "/", "", map[string]string{"Sec-Fetch-Site": "cross-site"}); res.StatusCode != http.StatusOK {
		t.Fatalf("a cross-site navigation to the page itself is allowed: %d", res.StatusCode)
	}
	if res := do(t, srv, http.MethodGet, "/assets", "", nil); res.StatusCode == http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		if strings.Contains(string(body), "<pre>") {
			t.Fatal("directory listing served")
		}
	}
}

func TestHandlerRefusesShortToken(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("a short token must be refused")
		}
	}()
	Handler(Deps{Addr: "127.0.0.1:8760", Token: "short", Store: flow.NewStore(1), Proxy: &fakeProxy{}, CA: fakeCA{}, Machine: &fakeMachine{}, Dist: fstest.MapFS{}})
}
