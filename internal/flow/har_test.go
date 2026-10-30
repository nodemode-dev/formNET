package flow

import (
	"encoding/json"
	"testing"
	"time"
)

func TestHARShapeAndTunnelsLeftOut(t *testing.T) {
	start := time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)
	f := Flow{ID: "1", Kind: KindHTTP, Start: start, Method: "POST", Protocol: "HTTP/1.1", Status: 302, ConnectMS: 10, TTFBMS: 40, DurationMS: 70,
		Request:  Message{Headers: []Header{{"Content-Type", "application/json"}}, HeaderBytes: 120, BodyBytes: 2, Body: []byte("{}"), ContentType: "application/json"},
		Response: &Message{Headers: []Header{{"Location", "https://x.example/next"}}, HeaderBytes: 80, BodyBytes: 4, Body: []byte{0xff, 0xfe, 0x00, 0x01}, ContentType: "application/octet-stream", BodyTruncated: true},
	}.WithURL("https://x.example/login?next=%2Fhome&a=b")
	tunnel := Flow{ID: "2", Kind: KindTunnel, Host: "pinned.example"}

	raw, err := MarshalHAR([]Flow{f, tunnel}, "v0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	log := doc["log"].(map[string]any)
	if log["version"] != "1.2" {
		t.Fatalf("version %v", log["version"])
	}
	entries := log["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("tunnels must be left out, got %d entries", len(entries))
	}
	e := entries[0].(map[string]any)
	req := e["request"].(map[string]any)
	res := e["response"].(map[string]any)
	if req["method"] != "POST" || req["url"] != f.URL || len(req["queryString"].([]any)) != 2 {
		t.Fatalf("request: %v", req)
	}
	if req["postData"].(map[string]any)["text"] != "{}" {
		t.Fatalf("postData: %v", req["postData"])
	}
	if res["status"].(float64) != 302 || res["redirectURL"] != "https://x.example/next" {
		t.Fatalf("response: %v", res)
	}
	content := res["content"].(map[string]any)
	if content["encoding"] != "base64" || content["text"] != "//4AAQ==" || content["size"].(float64) != 4 || content["comment"] == "" {
		t.Fatalf("binary body must be base64 with a truncation note: %v", content)
	}
	timings := e["timings"].(map[string]any)
	if timings["connect"].(float64) != 10 || timings["wait"].(float64) != 40 || timings["receive"].(float64) != 20 {
		t.Fatalf("timings: %v", timings)
	}
	if e["startedDateTime"] != "2026-10-30T10:00:00Z" || e["time"].(float64) != 70 {
		t.Fatalf("entry: %v %v", e["startedDateTime"], e["time"])
	}
}

func TestHARPendingFlowHasEmptyResponse(t *testing.T) {
	f := Flow{ID: "1", Kind: KindHTTP, Pending: true, Method: "GET"}.WithURL("http://a.example/")
	h := ToHAR([]Flow{f}, "dev")
	if len(h.Log.Entries) != 1 || h.Log.Entries[0].Response.Status != 0 || h.Log.Entries[0].Response.HeadersSize != -1 {
		t.Fatalf("%+v", h.Log.Entries)
	}
}
