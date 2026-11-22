package proxy

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"

	"github.com/nodemode-dev/formnet/internal/flow"
)

// ResendClient is the client process name a resent flow carries.
const ResendClient = "FormNET resend"

// resendTimeout bounds one resent exchange from start to the end of its response.
const resendTimeout = 10 * time.Minute

// ResendEdit changes a captured request before it goes out again; a nil
// field keeps what was captured. Body is the content as the flow detail
// shows it (decoded, when the detail says decoded).
type ResendEdit struct {
	Method  *string
	URL     *string
	Headers *[]flow.Header
	Body    *[]byte
}

var (
	// ErrNotResendable: the flow is not an HTTP exchange with a captured request.
	ErrNotResendable = errors.New("only an HTTP flow with a captured request can be sent again")
	// ErrBodyTruncated: the captured body is a prefix, so the request cannot go out unedited.
	ErrBodyTruncated = errors.New("the request body was larger than what FormNET kept, so it cannot be sent again as it was; it can be sent with an edited body")
)

// ResendInputError is an edit that cannot be sent.
type ResendInputError struct{ Reason string }

func (e *ResendInputError) Error() string { return e.Reason }

func badInput(reason string) error { return &ResendInputError{Reason: reason} }

// Resend sends a captured request again through the proxy's own pipeline and
// dialer, under the same destination rules as proxied traffic, and records
// the exchange as a new flow. It returns the new flow's id as soon as the
// flow is stored; the exchange goes on in the background.
func (p *Proxy) Resend(orig flow.Flow, e ResendEdit) (string, error) {
	if orig.Kind != flow.KindHTTP || orig.Method == "" || orig.URL == "" {
		return "", ErrNotResendable
	}
	edited := false
	method := orig.Method
	if e.Method != nil {
		m := strings.ToUpper(strings.TrimSpace(*e.Method))
		if !token(m) || m == http.MethodConnect {
			return "", badInput("method must be an HTTP method such as GET or POST")
		}
		edited = edited || m != orig.Method
		method = m
	}
	rawURL := orig.URL
	if e.URL != nil {
		rawURL = strings.TrimSpace(*e.URL)
		edited = edited || rawURL != orig.URL
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return "", badInput("url must be an absolute http or https URL")
	}
	headers := orig.Request.Headers
	if e.Headers != nil {
		edited = edited || !sameHeaders(*e.Headers, orig.Request.Headers)
		headers = *e.Headers
	}
	h := http.Header{}
	for _, x := range headers {
		name := strings.TrimSpace(x.Name)
		if !token(name) {
			return "", badInput("header name " + strconv.Quote(x.Name) + " is not valid")
		}
		if strings.ContainsAny(x.Value, "\r\n\x00") {
			return "", badInput("the value of header " + name + " holds a line break")
		}
		h.Add(name, x.Value)
	}
	host := h.Get("Host")
	h.Del("Host")
	var body []byte
	if e.Body != nil {
		edited = edited || orig.Request.BodyTruncated || !bytes.Equal(*e.Body, orig.Request.Body)
		body = *e.Body
	} else {
		if orig.Request.BodyTruncated {
			return "", ErrBodyTruncated
		}
		body = orig.Request.Body
	}
	if enc := h.Get("Content-Encoding"); orig.Request.Decoded && enc != "" && enc == orig.Request.Encoding {
		if body, err = encode(body, enc); err != nil {
			return "", badInput("the body could not be encoded as " + enc + ": " + err.Error())
		}
	}
	h = forwardable(h, false)
	h.Del("Content-Length")
	if len(body) > 0 {
		h.Set("Content-Length", strconv.Itoa(len(body)))
	}

	port := portOr(u.Port(), u.Scheme)
	if port <= 0 || port > 65535 {
		return "", badInput("url has no valid port")
	}
	hostPort := net.JoinHostPort(u.Hostname(), strconv.Itoa(port))
	check, cancelCheck := context.WithTimeout(context.Background(), dialTimeout)
	_, err = p.guard.resolve(check, hostPort)
	cancelCheck()
	var refused *RefusedError
	if errors.As(err, &refused) {
		return "", refused
	}

	ctx, cancel := context.WithTimeout(p.baseContext(), resendTimeout)
	r, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if err != nil {
		cancel()
		return "", badInput("the request could not be built: " + err.Error())
	}
	r.Header = h
	r.Host = u.Host
	if host != "" {
		r.Host = host
	}
	o := origin{scheme: u.Scheme, host: u.Hostname(), port: port}
	added := make(chan string, 1)
	hook := &flowHook{
		mark: func(f flow.Flow) flow.Flow {
			f.ResentFrom, f.Edited = orig.ID, edited
			f.ClientPID, f.ClientProcess = os.Getpid(), ResendClient
			return f
		},
		added: added,
	}
	go func() {
		defer cancel()
		defer close(added)
		defer func() {
			if v := recover(); v != nil {
				p.log.Warn("resend stopped", "from", orig.ID, "err", v)
			}
		}()
		_ = p.exchange(&discardWriter{h: http.Header{}}, r, o, hook)
	}()
	id, ok := <-added
	if !ok {
		return "", errors.New("the request could not be sent")
	}
	return id, nil
}

func (p *Proxy) baseContext() context.Context {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.serveCtx != nil {
		return p.serveCtx
	}
	return context.Background()
}

func sameHeaders(a, b []flow.Header) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !strings.EqualFold(strings.TrimSpace(a[i].Name), b[i].Name) || a[i].Value != b[i].Value {
			return false
		}
	}
	return true
}

// token reports whether s is an HTTP token (RFC 9110), as method and header names must be.
func token(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0:
		default:
			return false
		}
	}
	return true
}

// encode applies a Content-Encoding list, first listed first, to a body
// that was captured decoded.
func encode(body []byte, encoding string) ([]byte, error) {
	out := body
	for _, layer := range strings.Split(encoding, ",") {
		var buf bytes.Buffer
		var err error
		switch name := strings.ToLower(strings.TrimSpace(layer)); name {
		case "", "identity":
			continue
		case "gzip", "x-gzip":
			w := gzip.NewWriter(&buf)
			if _, err = w.Write(out); err == nil {
				err = w.Close()
			}
		case "deflate":
			w := zlib.NewWriter(&buf)
			if _, err = w.Write(out); err == nil {
				err = w.Close()
			}
		case "br":
			w := brotli.NewWriter(&buf)
			if _, err = w.Write(out); err == nil {
				err = w.Close()
			}
		case "zstd":
			var w *zstd.Encoder
			if w, err = zstd.NewWriter(nil); err == nil {
				buf.Write(w.EncodeAll(out, nil))
				err = w.Close()
			}
		default:
			return nil, errors.New("FormNET does not know the coding " + name)
		}
		if err != nil {
			return nil, err
		}
		out = buf.Bytes()
	}
	return out, nil
}

// discardWriter takes the response of a resent request, which has no client to go to.
type discardWriter struct {
	h http.Header
}

func (d *discardWriter) Header() http.Header         { return d.h }
func (d *discardWriter) Write(b []byte) (int, error) { return len(b), nil }
func (d *discardWriter) WriteHeader(int)             {}
func (d *discardWriter) Flush()                      {}
