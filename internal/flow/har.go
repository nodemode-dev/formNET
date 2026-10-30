package flow

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

// HAR is the HTTP Archive 1.2 shape browsers and proxies exchange.
type HAR struct {
	Log harLog `json:"log"`
}

type harLog struct {
	Version string     `json:"version"`
	Creator harCreator `json:"creator"`
	Entries []harEntry `json:"entries"`
}

type harCreator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type harEntry struct {
	StartedDateTime string      `json:"startedDateTime"`
	Time            float64     `json:"time"`
	Request         harRequest  `json:"request"`
	Response        harResponse `json:"response"`
	Cache           struct{}    `json:"cache"`
	Timings         harTimings  `json:"timings"`
	ServerIPAddress string      `json:"serverIPAddress,omitempty"`
	Comment         string      `json:"comment,omitempty"`
}

type harNameValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type harRequest struct {
	Method      string         `json:"method"`
	URL         string         `json:"url"`
	HTTPVersion string         `json:"httpVersion"`
	Cookies     []harNameValue `json:"cookies"`
	Headers     []harNameValue `json:"headers"`
	QueryString []harNameValue `json:"queryString"`
	PostData    *harPostData   `json:"postData,omitempty"`
	HeadersSize int64          `json:"headersSize"`
	BodySize    int64          `json:"bodySize"`
}

type harPostData struct {
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
	Encoding string `json:"encoding,omitempty"`
}

type harResponse struct {
	Status      int            `json:"status"`
	StatusText  string         `json:"statusText"`
	HTTPVersion string         `json:"httpVersion"`
	Cookies     []harNameValue `json:"cookies"`
	Headers     []harNameValue `json:"headers"`
	Content     harContent     `json:"content"`
	RedirectURL string         `json:"redirectURL"`
	HeadersSize int64          `json:"headersSize"`
	BodySize    int64          `json:"bodySize"`
}

type harContent struct {
	Size     int64  `json:"size"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text,omitempty"`
	Encoding string `json:"encoding,omitempty"`
	Comment  string `json:"comment,omitempty"`
}

type harTimings struct {
	Blocked float64 `json:"blocked"`
	DNS     float64 `json:"dns"`
	Connect float64 `json:"connect"`
	Send    float64 `json:"send"`
	Wait    float64 `json:"wait"`
	Receive float64 `json:"receive"`
	SSL     float64 `json:"ssl"`
}

// ToHAR converts HTTP flows to a HAR document. Tunnels have no HTTP shape and are left out.
func ToHAR(flows []Flow, creatorVersion string) HAR {
	entries := make([]harEntry, 0, len(flows))
	for _, f := range flows {
		if f.Kind == KindTunnel {
			continue
		}
		entries = append(entries, toEntry(f))
	}
	return HAR{Log: harLog{Version: "1.2", Creator: harCreator{Name: "formnet", Version: creatorVersion}, Entries: entries}}
}

// MarshalHAR writes flows as indented HAR JSON.
func MarshalHAR(flows []Flow, creatorVersion string) ([]byte, error) {
	return json.MarshalIndent(ToHAR(flows, creatorVersion), "", "  ")
}

func toEntry(f Flow) harEntry {
	req := harRequest{
		Method:      f.Method,
		URL:         f.URL,
		HTTPVersion: f.Protocol,
		Cookies:     []harNameValue{},
		Headers:     nameValues(f.Request.Headers),
		QueryString: queryValues(f.Query),
		HeadersSize: sizeOrUnknown(f.Request.HeaderBytes),
		BodySize:    sizeOrUnknown(f.Request.BodyBytes),
	}
	if len(f.Request.Body) > 0 {
		text, enc := bodyText(f.Request.Body)
		req.PostData = &harPostData{MimeType: f.Request.ContentType, Text: text, Encoding: enc}
	}
	res := harResponse{
		Status:      f.Status,
		StatusText:  statusText(f.Status),
		HTTPVersion: f.Protocol,
		Cookies:     []harNameValue{},
		Headers:     []harNameValue{},
		RedirectURL: "",
		HeadersSize: -1,
		BodySize:    -1,
		Content:     harContent{Size: 0, MimeType: ""},
	}
	if f.Response != nil {
		res.Headers = nameValues(f.Response.Headers)
		res.HeadersSize = sizeOrUnknown(f.Response.HeaderBytes)
		res.BodySize = sizeOrUnknown(f.Response.BodyBytes)
		res.Content = harContent{Size: f.Response.BodyBytes, MimeType: f.Response.ContentType}
		res.RedirectURL = headerValue(f.Response.Headers, "Location")
		if len(f.Response.Body) > 0 {
			res.Content.Text, res.Content.Encoding = bodyText(f.Response.Body)
			if f.Response.BodyTruncated {
				res.Content.Comment = "truncated by formnet's body limit"
			}
		}
	}
	wait := f.TTFBMS
	receive := f.DurationMS - f.ConnectMS - f.TTFBMS
	if receive < 0 {
		receive = 0
	}
	e := harEntry{
		StartedDateTime: f.Start.UTC().Format(time.RFC3339Nano),
		Time:            f.DurationMS,
		Request:         req,
		Response:        res,
		Timings:         harTimings{Blocked: -1, DNS: -1, Connect: f.ConnectMS, Send: 0, Wait: wait, Receive: receive, SSL: -1},
	}
	if f.Error != "" {
		e.Comment = "error: " + f.Error
	}
	return e
}

func sizeOrUnknown(n int64) int64 {
	if n <= 0 {
		return -1
	}
	return n
}

func nameValues(h []Header) []harNameValue {
	out := make([]harNameValue, len(h))
	for i, x := range h {
		out[i] = harNameValue{Name: x.Name, Value: x.Value}
	}
	return out
}

func queryValues(raw string) []harNameValue {
	out := []harNameValue{}
	if raw == "" {
		return out
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return out
	}
	for name, vs := range values {
		for _, v := range vs {
			out = append(out, harNameValue{Name: name, Value: v})
		}
	}
	return out
}

func headerValue(h []Header, name string) string {
	for _, x := range h {
		if strings.EqualFold(x.Name, name) {
			return x.Value
		}
	}
	return ""
}

// bodyText returns a body as text when it is valid UTF-8, otherwise base64.
func bodyText(b []byte) (text, encoding string) {
	if utf8.Valid(b) {
		return string(b), ""
	}
	return base64.StdEncoding.EncodeToString(b), "base64"
}

func statusText(code int) string {
	switch {
	case code == 0:
		return ""
	case code >= 100 && code < 200:
		return "Informational"
	case code >= 200 && code < 300:
		return "OK"
	case code >= 300 && code < 400:
		return "Redirect"
	case code >= 400 && code < 500:
		return "Client Error"
	case code >= 500:
		return "Server Error"
	}
	return ""
}
