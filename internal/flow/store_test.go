package flow

import (
	"context"
	"testing"
	"time"
)

func sample(id, host string) Flow {
	return Flow{ID: id, Kind: KindHTTP, Start: time.Now(), Pending: true, Method: "GET", Host: host, URL: "https://" + host + "/path?a=1", Request: Message{Body: []byte("req")}}
}

func TestStoreAddUpdateGetAndEvict(t *testing.T) {
	s := NewStore(2)
	a := s.Add(sample("a", "a.example"))
	b := s.Add(sample("b", "b.example"))
	if a.Seq != 1 || b.Seq != 2 {
		t.Fatalf("seq %d %d", a.Seq, b.Seq)
	}
	done := b.Completed(b.Start.Add(30 * time.Millisecond))
	done.Status = 200
	done.Response = &Message{Body: []byte("res"), BodyBytes: 3}
	if _, ok := s.Update(done); !ok {
		t.Fatal("update of a kept flow failed")
	}
	got, ok := s.Get("b")
	if !ok || got.Seq != 2 || got.Status != 200 || got.Pending || got.DurationMS < 29 {
		t.Fatalf("get after update: %+v ok=%v", got, ok)
	}
	if string(got.Response.Body) != "res" || string(got.Request.Body) != "req" {
		t.Fatal("Get must keep bodies")
	}
	s.Add(sample("c", "c.example"))
	if _, ok := s.Get("a"); ok {
		t.Fatal("oldest flow should have been evicted")
	}
	if _, ok := s.Update(sample("a", "a.example")); ok {
		t.Fatal("an evicted flow must not come back through Update")
	}
	if s.Len() != 2 {
		t.Fatalf("len %d", s.Len())
	}
}

func TestListStripsBodiesAndFilters(t *testing.T) {
	s := NewStore(10)
	s.Add(sample("1", "api.example.com"))
	f := sample("2", "cdn.example.com")
	f.Kind = KindTunnel
	f.Pending = false
	f.Status = 0
	s.Add(f)
	g := sample("3", "www.other.org")
	g.Method = "POST"
	g.ClientProcess = "Safari"
	s.Add(g)

	all := s.List(Query{})
	if len(all) != 3 || all[0].Request.Body != nil {
		t.Fatalf("list: %d flows, body %v", len(all), all[0].Request.Body)
	}
	if got := s.List(Query{Host: "*.example.com"}); len(got) != 2 {
		t.Fatalf("suffix filter: %d", len(got))
	}
	if got := s.List(Query{Host: "api.example.com"}); len(got) != 1 || got[0].ID != "1" {
		t.Fatalf("exact host filter: %+v", got)
	}
	if got := s.List(Query{Domain: "example.com"}); len(got) != 2 {
		t.Fatalf("domain filter: %d", len(got))
	}
	if got := s.List(Query{Kind: KindTunnel}); len(got) != 1 || got[0].ID != "2" {
		t.Fatalf("kind filter: %+v", got)
	}
	if got := s.List(Query{Text: "post"}); len(got) != 1 || got[0].ID != "3" {
		t.Fatalf("text filter: %+v", got)
	}
	if got := s.List(Query{Text: "safari"}); len(got) != 1 {
		t.Fatalf("process text filter: %+v", got)
	}
	pending := true
	if got := s.List(Query{Pending: &pending}); len(got) != 2 {
		t.Fatalf("pending filter: %d", len(got))
	}
	if got := s.List(Query{SinceSeq: 2}); len(got) != 1 || got[0].ID != "3" {
		t.Fatalf("since filter: %+v", got)
	}
	if got := s.List(Query{Limit: 2}); len(got) != 2 || got[0].ID != "2" {
		t.Fatalf("limit keeps the newest: %+v", got)
	}
}

func TestSubscribeAndClear(t *testing.T) {
	s := NewStore(10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := s.Subscribe(ctx)
	s.Add(sample("1", "a.example"))
	s.Update(sample("1", "a.example"))
	s.Clear()
	want := []EventType{EventAdded, EventUpdated, EventCleared}
	for _, w := range want {
		select {
		case e := <-ch:
			if e.Type != w {
				t.Fatalf("event %q, want %q", e.Type, w)
			}
		case <-time.After(time.Second):
			t.Fatalf("no %q event", w)
		}
	}
	if s.Len() != 0 {
		t.Fatal("clear left flows behind")
	}
	cancel()
	time.Sleep(10 * time.Millisecond)
	s.Add(sample("2", "b.example"))
	select {
	case e, ok := <-ch:
		if ok {
			t.Fatalf("event after cancel: %+v", e)
		}
	default:
	}
}

func TestHostsAggregatesAndSorts(t *testing.T) {
	s := NewStore(10)
	base := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	a := sample("1", "www.google-analytics.com")
	a.Start = base
	a.BytesSent, a.BytesReceived = 100, 50
	a.ClientProcess = "Chrome"
	s.Add(a)
	b := sample("2", "www.google-analytics.com")
	b.Start = base.Add(time.Minute)
	b.Status = 503
	b.Pending = false
	b.ClientProcess = "Slack"
	s.Add(b)
	c := sample("3", "pinned.example")
	c.Kind = KindTunnel
	c.BytesSent, c.BytesReceived = 5000, 9000
	c.Pending = false
	s.Add(c)

	hosts := s.Hosts()
	if len(hosts) != 2 {
		t.Fatalf("hosts: %d", len(hosts))
	}
	if hosts[0].Host != "pinned.example" || hosts[0].Intercepted || hosts[0].BytesReceived != 9000 {
		t.Fatalf("most traffic first, tunnel not intercepted: %+v", hosts[0])
	}
	ga := hosts[1]
	if ga.Flows != 2 || ga.Pending != 1 || ga.Errors != 1 || ga.BytesSent != 100 || !ga.Intercepted {
		t.Fatalf("aggregate: %+v", ga)
	}
	if ga.Category != "analytics" || ga.Owner != "Google" || ga.Domain != "google-analytics.com" {
		t.Fatalf("classification: %+v", ga)
	}
	if len(ga.Processes) != 2 || ga.Processes[0] != "Chrome" || ga.Processes[1] != "Slack" {
		t.Fatalf("processes: %v", ga.Processes)
	}
	if !ga.FirstSeen.Equal(base) || !ga.LastSeen.Equal(base.Add(time.Minute)) {
		t.Fatalf("seen window: %v %v", ga.FirstSeen, ga.LastSeen)
	}
}

func TestDomainAndHostMatches(t *testing.T) {
	cases := map[string]string{
		"www.example.com":       "example.com",
		"a.b.c.example.co.uk":   "example.co.uk",
		"example.com":           "example.com",
		"localhost":             "localhost",
		"cdn.shop.com.au":       "shop.com.au",
		"static.ads.bigcorp.io": "bigcorp.io",
		"":                      "",
	}
	for host, want := range cases {
		if got := Domain(host); got != want {
			t.Errorf("Domain(%q) = %q, want %q", host, got, want)
		}
	}
	if !HostMatches("API.Example.com", "*.example.com") || !HostMatches("example.com", "*.example.com") {
		t.Error("suffix pattern should match the suffix and its subdomains")
	}
	if HostMatches("notexample.com", "*.example.com") || HostMatches("a.example.com", "example.com") {
		t.Error("pattern matched too widely")
	}
}

func TestWithURL(t *testing.T) {
	f := Flow{}.WithURL("https://Example.com:8443/a/b?x=1")
	if f.Scheme != "https" || f.Host != "Example.com" || f.Port != 8443 || f.Path != "/a/b" || f.Query != "x=1" {
		t.Fatalf("%+v", f)
	}
	g := Flow{}.WithURL("http://plain.example/")
	if g.Port != 80 {
		t.Fatalf("default port: %+v", g)
	}
}
