package proxy

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/nodemode-dev/formnet/internal/flow"
)

// hopByHop headers describe one connection and are never forwarded.
var hopByHop = map[string]bool{
	"Connection":          true,
	"Keep-Alive":          true,
	"Proxy-Authenticate":  true,
	"Proxy-Authorization": true,
	"Proxy-Connection":    true,
	"Te":                  true,
	"Trailer":             true,
	"Transfer-Encoding":   true,
	"Upgrade":             true,
}

// forwardable copies h without hop-by-hop headers, the headers the
// Connection header names and every Proxy-* header. keepUpgrade keeps
// Connection and Upgrade for a WebSocket handshake.
func forwardable(h http.Header, keepUpgrade bool) http.Header {
	drop := map[string]bool{}
	for _, v := range h.Values("Connection") {
		for _, tok := range strings.Split(v, ",") {
			drop[http.CanonicalHeaderKey(strings.TrimSpace(tok))] = true
		}
	}
	out := make(http.Header, len(h))
	for name, values := range h {
		canon := http.CanonicalHeaderKey(name)
		if strings.HasPrefix(canon, "Proxy-") {
			continue
		}
		if keepUpgrade && (canon == "Connection" || canon == "Upgrade") {
			out[canon] = append([]string(nil), values...)
			continue
		}
		if hopByHop[canon] || drop[canon] {
			continue
		}
		out[canon] = append([]string(nil), values...)
	}
	return out
}

// orderedHeaders flattens h by name. net/http keeps no wire order, so the
// names are sorted; values of one name keep their order.
func orderedHeaders(h http.Header) []flow.Header {
	names := make([]string, 0, len(h))
	for name := range h {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]flow.Header, 0, len(h))
	for _, name := range names {
		for _, v := range h[name] {
			out = append(out, flow.Header{Name: name, Value: v})
		}
	}
	return out
}

// headerBytes approximates the size of a header block on the wire.
func headerBytes(firstLine string, h []flow.Header) int64 {
	n := int64(len(firstLine) + 2)
	for _, x := range h {
		n += int64(len(x.Name) + 2 + len(x.Value) + 2)
	}
	return n + 2
}

// capture counts every byte that passes and keeps the first limit of them.
type capture struct {
	r     io.Reader
	limit int64

	mu    sync.Mutex
	buf   bytes.Buffer
	total int64
	err   error
}

func newCapture(r io.Reader, limit int64) *capture {
	return &capture{r: r, limit: limit}
}

func (c *capture) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		c.mu.Lock()
		c.total += int64(n)
		if room := c.limit - int64(c.buf.Len()); room > 0 {
			if int64(n) > room {
				c.buf.Write(p[:room])
			} else {
				c.buf.Write(p[:n])
			}
		}
		c.mu.Unlock()
	}
	if err != nil && err != io.EOF {
		c.mu.Lock()
		c.err = err
		c.mu.Unlock()
	}
	return n, err
}

// Close closes the underlying reader when it has a Close method.
func (c *capture) Close() error {
	if cl, ok := c.r.(io.Closer); ok {
		return cl.Close()
	}
	return nil
}

// message builds the record of one side from the captured bytes.
func (c *capture) message(h http.Header, firstLine string, limit int64) flow.Message {
	c.mu.Lock()
	body := append([]byte(nil), c.buf.Bytes()...)
	total := c.total
	c.mu.Unlock()
	headers := orderedHeaders(h)
	encoding := h.Get("Content-Encoding")
	truncated := total > int64(len(body))
	if !truncated && encoding != "" {
		if decoded, ok := decode(body, encoding, limit); ok {
			body, truncated = decoded.body, decoded.truncated
		}
	}
	return flow.Message{
		Headers:       headers,
		HeaderBytes:   headerBytes(firstLine, headers),
		BodyBytes:     total,
		Body:          body,
		BodyTruncated: truncated,
		ContentType:   h.Get("Content-Type"),
		Encoding:      encoding,
	}
}

type decoded struct {
	body      []byte
	truncated bool
}

// decode inflates a gzip or deflate body that was captured whole, up to limit bytes.
func decode(body []byte, encoding string, limit int64) (decoded, bool) {
	var r io.ReadCloser
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "gzip", "x-gzip":
		gr, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return decoded{}, false
		}
		r = gr
	case "deflate":
		// Servers send deflate both as zlib-wrapped (the standard) and raw.
		if zr, err := zlib.NewReader(bytes.NewReader(body)); err == nil {
			r = zr
		} else {
			r = flate.NewReader(bytes.NewReader(body))
		}
	default:
		return decoded{}, false
	}
	defer r.Close()
	out, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil && len(out) == 0 {
		return decoded{}, false
	}
	if int64(len(out)) > limit {
		return decoded{body: out[:limit], truncated: true}, true
	}
	return decoded{body: out}, true
}

// counter counts bytes copied through it.
type counter struct {
	mu sync.Mutex
	n  int64
}

func (c *counter) Write(p []byte) (int, error) {
	c.mu.Lock()
	c.n += int64(len(p))
	c.mu.Unlock()
	return len(p), nil
}

func (c *counter) Total() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}
