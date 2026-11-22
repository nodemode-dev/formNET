package flow

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"
)

// HARImport is what ImportHAR made of a HAR file.
type HARImport struct {
	Creator  string
	Flows    []Flow
	Messages map[string][]SessionMessage // Chrome's _webSocketMessages, by flow id
	Skipped  int                         // entries without a method or an absolute URL
	Dropped  int                         // the oldest entries left out to stay inside the Store's limits
}

type harInEntry struct {
	StartedDateTime string  `json:"startedDateTime"`
	Time            float64 `json:"time"`
	Request         struct {
		Method      string         `json:"method"`
		URL         string         `json:"url"`
		HTTPVersion string         `json:"httpVersion"`
		Headers     []harNameValue `json:"headers"`
		HeadersSize int64          `json:"headersSize"`
		BodySize    int64          `json:"bodySize"`
		PostData    *struct {
			MimeType string `json:"mimeType"`
			Text     string `json:"text"`
			Encoding string `json:"encoding"`
			Comment  string `json:"comment"`
		} `json:"postData"`
	} `json:"request"`
	Response *struct {
		Status      int            `json:"status"`
		HTTPVersion string         `json:"httpVersion"`
		Headers     []harNameValue `json:"headers"`
		HeadersSize int64          `json:"headersSize"`
		BodySize    int64          `json:"bodySize"`
		Content     struct {
			Size     int64  `json:"size"`
			MimeType string `json:"mimeType"`
			Text     string `json:"text"`
			Encoding string `json:"encoding"`
			Comment  string `json:"comment"`
		} `json:"content"`
		Error string `json:"_error"`
	} `json:"response"`
	Timings *struct {
		Connect *float64 `json:"connect"`
		SSL     *float64 `json:"ssl"`
		Wait    *float64 `json:"wait"`
	} `json:"timings"`
	Comment      string `json:"comment"`
	ResourceType string `json:"_resourceType"`
	WSMessages   []struct {
		Type   string  `json:"type"`
		Time   float64 `json:"time"`
		Opcode int     `json:"opcode"`
		Data   string  `json:"data"`
	} `json:"_webSocketMessages"`
}

// ImportHAR reads a HAR 1.2 document (FormNET's, a browser's developer
// tools', Charles') into flows marked from_session and imported "har".
// Bodies are taken from text or base64 and cut at bodyLimit; values that
// were redacted in the file stay as they are. Entries are read one at a
// time, and past the Store's limits (lim.Flows, lim.BodyBytes) the oldest
// read are left out, so memory stays near the Store's budget. A document
// without log.entries is an error that says so plainly.
func ImportHAR(r io.Reader, bodyLimit int64, lim SessionLimits) (*HARImport, error) {
	if lim.MaxBytes > 0 {
		r = &cappedReader{r: r, left: lim.MaxBytes}
	}
	dec := json.NewDecoder(r)
	plain := func(err error) error {
		if errors.Is(err, ErrSessionTooLarge) {
			return err
		}
		return fmt.Errorf("the file is not a HAR file: it is not a JSON object with a log (%v)", err)
	}
	if err := expectDelim(dec, '{'); err != nil {
		return nil, plain(err)
	}
	out := &HARImport{Messages: map[string][]SessionMessage{}}
	seenEntries := false
	var kept int64
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, plain(err)
		}
		if tok != "log" {
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return nil, plain(err)
			}
			continue
		}
		if err := expectDelim(dec, '{'); err != nil {
			return nil, errors.New("the file is JSON but not a HAR file: log is not an object")
		}
		for dec.More() {
			tok, err := dec.Token()
			if err != nil {
				return nil, plain(err)
			}
			switch tok {
			case "creator":
				var c struct {
					Name    string `json:"name"`
					Version string `json:"version"`
				}
				if err := dec.Decode(&c); err != nil {
					return nil, fmt.Errorf("the HAR file's log.creator is not an object: %v", err)
				}
				out.Creator = strings.TrimSpace(c.Name + " " + c.Version)
			case "entries":
				if err := expectDelim(dec, '['); err != nil {
					return nil, errors.New("the HAR file's log.entries is not a list")
				}
				seenEntries = true
				for i := 1; dec.More(); i++ {
					var e harInEntry
					if err := dec.Decode(&e); err != nil {
						if errors.Is(err, ErrSessionTooLarge) {
							return nil, err
						}
						var typ *json.UnmarshalTypeError
						if errors.As(err, &typ) && typ.Field != "" {
							return nil, fmt.Errorf("entry %d of the HAR file does not have the HAR shape: %s is a JSON %s", i, typ.Field, typ.Value)
						}
						return nil, fmt.Errorf("entry %d of the HAR file does not have the HAR shape: %v", i, err)
					}
					f, msgs, ok := e.toFlow(bodyLimit)
					if !ok {
						out.Skipped++
						continue
					}
					out.Flows = append(out.Flows, f)
					kept += captured(&f)
					if len(msgs) > 0 {
						out.Messages[f.ID] = msgs
					}
					for len(out.Flows) > 1 && ((lim.Flows > 0 && len(out.Flows) > lim.Flows) || (lim.BodyBytes > 0 && kept > lim.BodyBytes)) {
						kept -= captured(&out.Flows[0])
						delete(out.Messages, out.Flows[0].ID)
						out.Flows = out.Flows[1:]
						out.Dropped++
					}
				}
				if err := expectDelim(dec, ']'); err != nil {
					return nil, plain(err)
				}
			default:
				var skip json.RawMessage
				if err := dec.Decode(&skip); err != nil {
					return nil, plain(err)
				}
			}
		}
		if err := expectDelim(dec, '}'); err != nil {
			return nil, plain(err)
		}
	}
	if !seenEntries {
		return nil, errors.New("the file is JSON but not a HAR file: it has no log.entries list")
	}
	return out, nil
}

func (e harInEntry) toFlow(limit int64) (Flow, []SessionMessage, bool) {
	method := strings.ToUpper(strings.TrimSpace(e.Request.Method))
	f := Flow{ID: newEpoch(), Kind: KindHTTP, Method: method, FromSession: true, Imported: "har"}.WithURL(e.Request.URL)
	if method == "" || f.Host == "" || f.Scheme == "" {
		return f, nil, false
	}
	start, err := time.Parse(time.RFC3339Nano, e.StartedDateTime)
	if err != nil {
		start = time.Unix(0, 0).UTC()
	}
	f.Start = start
	if e.Time >= 0 && !math.IsNaN(e.Time) {
		f.DurationMS = e.Time
		end := start.Add(time.Duration(e.Time * float64(time.Millisecond)))
		f.End = &end
	}
	f.ClientProto = e.Request.HTTPVersion
	f.ClientProtocol = e.Request.HTTPVersion
	f.Request = Message{Headers: harHeaders(e.Request.Headers), HeaderBytes: max(e.Request.HeadersSize, 0)}
	f.Request.ContentType = headerValue(f.Request.Headers, "Content-Type")
	f.Request.Encoding = headerValue(f.Request.Headers, "Content-Encoding")
	if pd := e.Request.PostData; pd != nil {
		if f.Request.ContentType == "" {
			f.Request.ContentType = pd.MimeType
		}
		b64 := strings.EqualFold(pd.Encoding, "base64") || strings.Contains(strings.ToLower(pd.Comment), "base64")
		f.Request.Body = harBody(pd.Text, b64)
		f.Request.Decoded = f.Request.Encoding != "" && !strings.Contains(strings.ToLower(pd.Comment), "still")
		f.Request.BodyTruncated = strings.Contains(strings.ToLower(pd.Comment), "truncated")
	}
	f.Request.BodyBytes = e.Request.BodySize
	if f.Request.BodyBytes < int64(len(f.Request.Body)) {
		f.Request.BodyBytes = int64(len(f.Request.Body))
	}
	cut(&f.Request, limit)
	if r := e.Response; r != nil {
		resp := Message{Headers: harHeaders(r.Headers), HeaderBytes: max(r.HeadersSize, 0)}
		resp.ContentType = r.Content.MimeType
		if resp.ContentType == "" {
			resp.ContentType = headerValue(resp.Headers, "Content-Type")
		}
		resp.Encoding = headerValue(resp.Headers, "Content-Encoding")
		resp.Body = harBody(r.Content.Text, strings.EqualFold(r.Content.Encoding, "base64"))
		resp.Decoded = resp.Encoding != "" && len(resp.Body) > 0 && !strings.Contains(strings.ToLower(r.Content.Comment), "still")
		resp.BodyBytes = r.BodySize
		if resp.BodyBytes <= 0 {
			resp.BodyBytes = r.Content.Size
		}
		if resp.BodyBytes < int64(len(resp.Body)) {
			resp.BodyBytes = int64(len(resp.Body))
		}
		if strings.Contains(strings.ToLower(r.Content.Comment), "truncated") {
			resp.BodyTruncated = true
		}
		cut(&resp, limit)
		f.Status = r.Status
		f.ServerProto = r.HTTPVersion
		f.Protocol = r.HTTPVersion
		if r.Status > 0 || len(resp.Headers) > 0 {
			f.Response = &resp
		}
		if r.Error != "" {
			f.Error = r.Error
		}
	}
	if msg, ok := strings.CutPrefix(e.Comment, "error: "); ok && f.Error == "" {
		f.Error = msg
	}
	if t := e.Timings; t != nil {
		if t.Connect != nil && *t.Connect > 0 {
			f.ConnectMS = *t.Connect
		}
		if t.Wait != nil && *t.Wait > 0 {
			f.TTFBMS = *t.Wait
		}
	}
	f.BytesSent = f.Request.HeaderBytes + f.Request.BodyBytes
	if f.Response != nil {
		f.BytesReceived = f.Response.HeaderBytes + f.Response.BodyBytes
	}
	var msgs []SessionMessage
	if len(e.WSMessages) > 0 || strings.EqualFold(e.ResourceType, "websocket") || f.Status == 101 {
		f.Kind = KindWebSocket
		sum := MessageSummary{}
		for _, m := range e.WSMessages {
			sm := SessionMessage{Dir: "server", Opcode: wsOpcode(m.Opcode)}
			if m.Type == "send" {
				sm.Dir = "client"
			}
			if m.Time > 0 {
				sec, frac := math.Modf(m.Time)
				sm.Time = time.Unix(int64(sec), int64(frac*1e9)).UTC()
			}
			if sm.Opcode == "binary" {
				d := m.Data
				sm.Base64 = &d
				if raw, err := base64.StdEncoding.DecodeString(d); err == nil {
					sm.Bytes = int64(len(raw))
				}
			} else {
				d := m.Data
				sm.Text = &d
				sm.Bytes = int64(len(d))
			}
			sum.Count++
			sum.Bytes += sm.Bytes
			sm.Seq = sum.Count
			if sm.Opcode == "close" && sum.ClosedBy == "" {
				sum.ClosedBy = sm.Dir
			}
			msgs = append(msgs, sm)
		}
		f.Messages = &sum
	}
	return f, msgs, true
}

func wsOpcode(op int) string {
	switch op {
	case 2:
		return "binary"
	case 8:
		return "close"
	case 9:
		return "ping"
	case 10:
		return "pong"
	}
	return "text"
}

// harHeaders keeps a HAR's headers sorted by name like captured ones, the
// HTTP/2 pseudo-headers (:method, :path) left out.
func harHeaders(in []harNameValue) []Header {
	out := make([]Header, 0, len(in))
	for _, h := range in {
		if h.Name == "" || strings.HasPrefix(h.Name, ":") {
			continue
		}
		out = append(out, Header{Name: h.Name, Value: h.Value})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func harBody(text string, b64 bool) []byte {
	if text == "" {
		return nil
	}
	if b64 {
		if raw, err := base64.StdEncoding.DecodeString(text); err == nil {
			return raw
		}
	}
	return []byte(text)
}

func cut(m *Message, limit int64) {
	if limit > 0 && int64(len(m.Body)) > limit {
		m.Body = m.Body[:limit]
		m.BodyTruncated = true
	}
}
