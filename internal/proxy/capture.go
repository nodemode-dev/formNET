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

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"

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
	wasDecoded := false
	if !truncated && encoding != "" {
		if decoded, ok := decode(body, encoding, limit); ok {
			body, truncated, wasDecoded = decoded.body, decoded.truncated, true
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
		Decoded:       wasDecoded,
	}
}

type decoded struct {
	body      []byte
	truncated bool
}

// stageFactor bounds an inner layer of a stacked encoding at this many times the body limit.
const stageFactor = 4

// zstdWindow is the largest zstd window decoded, the 8 MiB HTTP asks senders to stay within (RFC 9659).
const zstdWindow = 8 << 20

// decode undoes the Content-Encoding of a body that was captured whole: gzip,
// deflate, br and zstd, and a list of them, last applied first. The output is
// cut at limit bytes, and an inner layer at stageFactor times limit, so a
// small body that inflates without end never grows past that. A body that
// does not decode is reported as not decoded and kept as captured.
func decode(body []byte, encoding string, limit int64) (decoded, bool) {
	layers := strings.Split(encoding, ",")
	out := body
	cut := false
	applied := 0
	for i := len(layers) - 1; i >= 0; i-- {
		name := strings.ToLower(strings.TrimSpace(layers[i]))
		if name == "" || name == "identity" {
			continue
		}
		room := limit
		if i > 0 {
			room = limit * stageFactor
		}
		next, over, ok := decodeOne(out, name, room)
		if !ok || (over && i > 0) {
			return decoded{}, false
		}
		out, cut = next, over
		applied++
	}
	if applied == 0 {
		return decoded{}, false
	}
	return decoded{body: out, truncated: cut}, true
}

// decodeOne undoes one coding, reading at most room bytes of output. It
// returns the output, whether it was cut at room, and whether it decoded.
func decodeOne(body []byte, name string, room int64) ([]byte, bool, bool) {
	var r io.Reader
	switch name {
	case "gzip", "x-gzip":
		gr, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, false, false
		}
		defer gr.Close()
		r = gr
	case "deflate":
		// Servers send deflate both as zlib-wrapped (the standard) and raw.
		if zr, err := zlib.NewReader(bytes.NewReader(body)); err == nil {
			defer zr.Close()
			r = zr
		} else {
			fr := flate.NewReader(bytes.NewReader(body))
			defer fr.Close()
			r = fr
		}
	case "br":
		r = brotli.NewReader(bytes.NewReader(body))
	case "zstd":
		zr, err := zstd.NewReader(bytes.NewReader(body),
			zstd.WithDecoderConcurrency(1),
			zstd.WithDecoderLowmem(true),
			zstd.WithDecoderMaxWindow(zstdWindow))
		if err != nil {
			return nil, false, false
		}
		defer zr.Close()
		r = zr
	default:
		return nil, false, false
	}
	out, err := io.ReadAll(io.LimitReader(r, room+1))
	if int64(len(out)) > room {
		return out[:room], true, true
	}
	if err != nil {
		return nil, false, false
	}
	return out, false, true
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
