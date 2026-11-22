package proxy

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nodemode-dev/formnet/internal/flow"
	"github.com/nodemode-dev/formnet/internal/rules"
)

// editLimit bounds a body a rule rewrites or a breakpoint holds; a larger
// body passes unchanged, with a note.
const editLimit = 8 << 20

// ruleExchange carries out a rule plan for one exchange and records it.
func (p *Proxy) ruleExchange(w http.ResponseWriter, r *http.Request, o origin, plan *rules.Plan, throttled bool, st *trafficState) {
	rawURL := o.url(r)
	f := p.newFlow(flow.KindHTTP, o, r.Method, rawURL)
	f.Rules = append([]string(nil), plan.Names...)
	f.Throttled = throttled
	f.ClientProtocol, f.Protocol = r.Proto, r.Proto
	requestLine := fmt.Sprintf("%s %s %s", r.Method, r.URL.RequestURI(), r.Proto)
	note := func(s string) { f.Notes = append(f.Notes, s) }
	emptyCapture := func() *capture { return newCapture(http.NoBody, 0) }

	target, err := url.Parse(rawURL)
	if err != nil {
		f = p.opts.Store.Add(f)
		p.fail(w, f, http.StatusBadRequest, "formnet could not read the request URL: "+err.Error())
		return
	}

	if t := plan.Terminal; t != nil && t.Action.Type == rules.ActionBlock {
		f.Request = emptyCapture().message(r.Header, requestLine, 0)
		f = p.opts.Store.Add(f)
		if t.Action.Drop {
			note("Dropped by the rule " + strconv.Quote(t.Name) + ": FormNET closed the connection without an answer.")
			f.Error = "dropped by FormNET rule " + strconv.Quote(t.Name)
			p.finish(f.Completed(time.Now()))
			panic(http.ErrAbortHandler)
		}
		note("Blocked by the rule " + strconv.Quote(t.Name) + ": FormNET answered 403 instead of the server.")
		resp := synthetic(http.StatusForbidden, "text/plain; charset=utf-8", []byte("Blocked by FormNET: the rule \""+t.Name+"\" matched this request.\n"))
		f, _ = p.relayResponse(w, resp, f, &timing{}, emptyCapture(), requestLine, r.Header)
		p.finish(f)
		return
	}

	outHeader := forwardable(r.Header, false)
	method := r.Method
	hostHeader := r.Host
	if hostHeader == "" {
		hostHeader = target.Host
	}

	needReqBody := plan.BreakReq != nil
	for _, c := range plan.Rewrites {
		needReqBody = needReqBody || c.HasBody("request")
	}
	body := r.Body
	if body == nil {
		body = http.NoBody
	}
	var reqBody []byte
	whole := false
	if needReqBody && body != http.NoBody {
		buf, err := io.ReadAll(io.LimitReader(body, editLimit+1))
		if err != nil {
			f = p.opts.Store.Add(f)
			p.fail(w, f, http.StatusBadRequest, "formnet could not read the request body: "+err.Error())
			return
		}
		if len(buf) > editLimit {
			note("The request body is larger than 8 MiB, so rules and breakpoints left it unchanged.")
			body = readCloser{Reader: io.MultiReader(bytes.NewReader(buf), r.Body), Closer: r.Body}
		} else {
			reqBody, whole = buf, true
		}
	} else if needReqBody {
		whole = true
	}

	for _, c := range plan.Rewrites {
		rules.ApplyHeaders(outHeader, c.Action.RequestHeaders)
		if c.ApplyURL(target) && c.Action.Host != "" {
			hostHeader = target.Host
		}
		if whole && c.HasBody("request") {
			if text, why := decodedText(reqBody, outHeader); why != "" {
				note("The rule " + strconv.Quote(c.Name) + " did not change the request body: " + why + ".")
			} else {
				if enc := outHeader.Get("Content-Encoding"); enc != "" {
					note("The request body was rewritten on its decoded form and sent without its Content-Encoding (" + enc + ").")
					outHeader.Del("Content-Encoding")
				}
				reqBody = c.ReplaceBody("request", text)
			}
		}
	}
	if h := outHeader.Get("Host"); h != "" {
		hostHeader = h
		outHeader.Del("Host")
	}
	if m := plan.MapRemote; m != nil {
		mapped := m.MapURL(target)
		if !m.Action.KeepHost {
			hostHeader = mapped.Host
		}
		note("Sent to " + mapped.String() + " by the rule " + strconv.Quote(m.Name) + ".")
		target = mapped
	}
	if !loopbackClient(o.client) && loopbackTarget(target.Hostname()) {
		f = p.opts.Store.Add(f)
		p.fail(w, f, http.StatusForbidden, "FormNET does not let another device reach this computer's loopback addresses")
		return
	}

	recordRequest := func() {
		c := newCapture(bytes.NewReader(reqBody), p.opts.BodyLimit)
		_, _ = io.Copy(io.Discard, c)
		f.Request = c.message(withHost(outHeader, hostHeader), requestLine, p.opts.BodyLimit)
	}
	if bp := plan.BreakReq; bp != nil && (whole || len(reqBody) == 0) {
		recordRequest()
		f.Paused = StageRequest
		f = p.opts.Store.Add(f)
		info := Paused{FlowID: f.ID, Stage: StageRequest, Rule: bp.Name, Method: method, URL: target.String(), Headers: orderedHeaders(withHost(outHeader, hostHeader))}
		shown, decoded := reqBody, false
		if text, why := decodedText(reqBody, outHeader); why == "" && outHeader.Get("Content-Encoding") != "" {
			shown, decoded = text, true
		}
		showBody(&info, shown, decoded)
		began := time.Now()
		d, out := st.bp.pause(r.Context(), info)
		f.Paused = ""
		switch out {
		case outcomeFull:
			note("Passed the breakpoint " + strconv.Quote(bp.Name) + " without pausing: as many exchanges as allowed were already paused.")
		case outcomeTimeout:
			note(fmt.Sprintf("Released after %s at the request breakpoint %q, which nobody continued.", time.Since(began).Round(time.Second), bp.Name))
		case outcomeGone:
			f.Error = "the client went away while the request was paused"
			p.finish(f.Completed(time.Now()))
			return
		case outcomeAbort:
			note("Aborted at the request breakpoint " + strconv.Quote(bp.Name) + ".")
			f.Error = "aborted at a FormNET breakpoint"
			p.finish(f.Completed(time.Now()))
			panic(http.ErrAbortHandler)
		case outcomeContinue:
			if d.edit.empty() {
				note("Continued at the request breakpoint " + strconv.Quote(bp.Name) + ".")
				break
			}
			note("Edited at the request breakpoint " + strconv.Quote(bp.Name) + ".")
			if d.edit.Method != nil {
				method = strings.ToUpper(strings.TrimSpace(*d.edit.Method))
				f.Method = method
			}
			if d.edit.URL != nil {
				target, _ = url.Parse(strings.TrimSpace(*d.edit.URL))
				hostHeader = target.Host
			}
			if d.edit.Headers != nil {
				h := http.Header{}
				for _, x := range *d.edit.Headers {
					h.Add(strings.TrimSpace(x.Name), x.Value)
				}
				if v := h.Get("Host"); v != "" {
					hostHeader = v
				}
				h.Del("Host")
				outHeader = forwardable(h, false)
			}
			if d.edit.Body != nil {
				reqBody = *d.edit.Body
				if decoded {
					outHeader.Del("Content-Encoding")
				}
			}
			if !loopbackClient(o.client) && loopbackTarget(target.Hostname()) {
				p.fail(w, f, http.StatusForbidden, "FormNET does not let another device reach this computer's loopback addresses")
				return
			}
		}
	}

	var reqCapture *capture
	if whole {
		outHeader.Del("Content-Length")
		reqCapture = newCapture(bytes.NewReader(reqBody), p.opts.BodyLimit)
	} else {
		reqCapture = newCapture(body, p.opts.BodyLimit)
	}
	recordHeader := withHost(outHeader, hostHeader)
	f.Request = reqCapture.message(recordHeader, requestLine, p.opts.BodyLimit)
	f = p.opts.Store.Add(f)

	var resp *http.Response
	tm := &timing{}
	if t := plan.Terminal; t != nil && t.Action.Type == rules.ActionMapLocal {
		_, _ = io.Copy(io.Discard, io.LimitReader(reqCapture, editLimit))
		resp = p.mapLocal(st, t, target.Path, note)
	} else {
		out, err := http.NewRequestWithContext(httptrace.WithClientTrace(r.Context(), tm.trace()), method, target.String(), reqCapture)
		if err != nil {
			p.fail(w, f, http.StatusBadRequest, "formnet could not build the upstream request: "+err.Error())
			return
		}
		out.Header = outHeader.Clone()
		keepTE(out.Header, r.Header, r.Trailer, out)
		if _, ok := out.Header["User-Agent"]; !ok {
			out.Header["User-Agent"] = []string{""} // net/http would add its own otherwise; a removed one stays removed
		}
		out.Host = hostHeader
		switch {
		case whole && len(reqBody) == 0, !whole && body == http.NoBody:
			out.Body, out.ContentLength = http.NoBody, 0
		case whole:
			out.ContentLength = int64(len(reqBody))
		default:
			out.ContentLength = r.ContentLength
		}
		resp, err = p.transport.RoundTrip(out)
		if err != nil {
			p.fail(w, f, statusFor(err), "formnet could not reach "+target.Hostname()+": "+err.Error())
			return
		}
	}
	defer resp.Body.Close()

	resp, ok := p.responseRules(w, r, resp, plan, st, &f, note)
	if !ok {
		return
	}
	f, copyErr := p.relayResponse(w, resp, f, tm, reqCapture, requestLine, recordHeader)
	p.finish(f)
	if copyErr != nil {
		panic(http.ErrAbortHandler)
	}
}

// responseRules applies the response side of a plan: status, headers, body
// find and replace, and the response breakpoint. ok false means the exchange
// ended here (aborted, or its client left).
func (p *Proxy) responseRules(w http.ResponseWriter, r *http.Request, resp *http.Response, plan *rules.Plan, st *trafficState, f *flow.Flow, note func(string)) (*http.Response, bool) {
	needBody := plan.BreakResp != nil
	changes := plan.BreakResp != nil
	for _, c := range plan.Rewrites {
		a := c.Action
		needBody = needBody || c.HasBody("response")
		changes = changes || c.HasBody("response") || a.Status != 0 || len(a.ResponseHeaders) > 0
	}
	if !changes {
		return resp, true
	}
	nr := *resp
	nr.Header = resp.Header.Clone()
	var body []byte
	whole, changed := false, false
	if needBody && r.Method != http.MethodHead {
		buf, err := io.ReadAll(io.LimitReader(resp.Body, editLimit+1))
		switch {
		case err != nil:
			note("The response body could not be read whole (" + err.Error() + "), so rules left it as it came.")
			nr.Body = io.NopCloser(io.MultiReader(bytes.NewReader(buf), errReader{err}))
		case len(buf) > editLimit:
			note("The response body is larger than 8 MiB, so rules and breakpoints left it unchanged.")
			nr.Body = io.NopCloser(io.MultiReader(bytes.NewReader(buf), resp.Body))
		default:
			body, whole = buf, true
		}
	}
	for _, c := range plan.Rewrites {
		if s := c.Action.Status; s != 0 {
			nr.StatusCode, nr.Status = s, statusText(s)
		}
		rules.ApplyHeaders(nr.Header, c.Action.ResponseHeaders)
		if whole && c.HasBody("response") {
			text, why := decodedText(body, nr.Header)
			if why != "" {
				note("The rule " + strconv.Quote(c.Name) + " did not change the response body: " + why + ".")
				continue
			}
			if enc := nr.Header.Get("Content-Encoding"); enc != "" {
				note("The response body was rewritten on its decoded form and sent without its Content-Encoding (" + enc + ").")
				nr.Header.Del("Content-Encoding")
			}
			body, changed = c.ReplaceBody("response", text), true
		}
	}
	if bp := plan.BreakResp; bp != nil {
		if !whole && needBody && r.Method != http.MethodHead {
			note("Passed the response breakpoint " + strconv.Quote(bp.Name) + " without pausing: the body could not be held.")
		} else {
			info := Paused{FlowID: f.ID, Stage: StageResponse, Rule: bp.Name, URL: f.URL, Status: nr.StatusCode, Headers: orderedHeaders(nr.Header)}
			shown, decoded := body, false
			if text, why := decodedText(body, nr.Header); why == "" && nr.Header.Get("Content-Encoding") != "" {
				shown, decoded = text, true
			}
			showBody(&info, shown, decoded)
			f.Paused = StageResponse
			f.Status = nr.StatusCode
			*f, _ = p.opts.Store.Update(*f)
			began := time.Now()
			d, out := st.bp.pause(r.Context(), info)
			f.Paused = ""
			switch out {
			case outcomeFull:
				note("Passed the breakpoint " + strconv.Quote(bp.Name) + " without pausing: as many exchanges as allowed were already paused.")
			case outcomeTimeout:
				note(fmt.Sprintf("Released after %s at the response breakpoint %q, which nobody continued.", time.Since(began).Round(time.Second), bp.Name))
			case outcomeGone:
				f.Error = "the client went away while the response was paused"
				p.finish(f.Completed(time.Now()))
				return nil, false
			case outcomeAbort:
				note("Aborted at the response breakpoint " + strconv.Quote(bp.Name) + ".")
				f.Error = "aborted at a FormNET breakpoint"
				p.finish(f.Completed(time.Now()))
				panic(http.ErrAbortHandler)
			case outcomeContinue:
				if d.edit.empty() {
					note("Continued at the response breakpoint " + strconv.Quote(bp.Name) + ".")
					break
				}
				note("Edited at the response breakpoint " + strconv.Quote(bp.Name) + ".")
				if d.edit.Status != nil {
					nr.StatusCode, nr.Status = *d.edit.Status, statusText(*d.edit.Status)
				}
				if d.edit.Headers != nil {
					h := http.Header{}
					for _, x := range *d.edit.Headers {
						h.Add(strings.TrimSpace(x.Name), x.Value)
					}
					nr.Header = h
				}
				if d.edit.Body != nil {
					body, changed = *d.edit.Body, true
					if decoded {
						nr.Header.Del("Content-Encoding")
					}
				}
			}
		}
	}
	if whole {
		nr.Body = io.NopCloser(bytes.NewReader(body))
		if changed {
			nr.ContentLength = int64(len(body))
			nr.Header.Set("Content-Length", strconv.Itoa(len(body)))
		}
	}
	return &nr, true
}

// mapLocal answers from the file a map_local rule names.
func (p *Proxy) mapLocal(st *trafficState, c *rules.Compiled, path string, note func(string)) *http.Response {
	lf, err := st.rules.ResolveLocal(c, path)
	switch {
	case errors.Is(err, rules.ErrLocalOutside):
		note("The rule " + strconv.Quote(c.Name) + " refused the path: it leaves the mapped folder.")
		return synthetic(http.StatusForbidden, "text/plain; charset=utf-8", []byte("FormNET map_local: that path leaves the mapped folder.\n"))
	case err != nil:
		note("The rule " + strconv.Quote(c.Name) + " found no file for this path.")
		return synthetic(http.StatusNotFound, "text/plain; charset=utf-8", []byte("FormNET map_local: no file for this path.\n"))
	}
	file, err := os.Open(lf.Path)
	if err == nil {
		if info, serr := file.Stat(); serr != nil || !os.SameFile(info, lf.Info) {
			_ = file.Close()
			err = errors.New("the file changed while it was opened")
		}
	}
	if err != nil {
		note("The rule " + strconv.Quote(c.Name) + " could not open the file: " + err.Error())
		return synthetic(http.StatusNotFound, "text/plain; charset=utf-8", []byte("FormNET map_local: no file for this path.\n"))
	}
	note("Answered from the local file " + lf.Path + " by the rule " + strconv.Quote(c.Name) + ".")
	resp := synthetic(http.StatusOK, lf.ContentType, nil)
	resp.Body = file
	resp.ContentLength = lf.Size
	resp.Header.Set("Content-Length", strconv.FormatInt(lf.Size, 10))
	return resp
}

// synthetic is a response FormNET gives itself.
func synthetic(status int, contentType string, body []byte) *http.Response {
	h := http.Header{}
	h.Set("Content-Type", contentType)
	h.Set("Content-Length", strconv.Itoa(len(body)))
	h.Set("Cache-Control", "no-store")
	return &http.Response{StatusCode: status, Status: statusText(status), Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1, Header: h, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body))}
}

func statusText(code int) string {
	if t := http.StatusText(code); t != "" {
		return strconv.Itoa(code) + " " + t
	}
	return strconv.Itoa(code)
}

func withHost(h http.Header, host string) http.Header {
	out := h.Clone()
	if host != "" {
		out.Set("Host", host)
	}
	return out
}

// decodedText returns the body as text a find and replace may change, or why not.
func decodedText(body []byte, h http.Header) ([]byte, string) {
	if !rules.IsText(h.Get("Content-Type")) {
		return nil, "it is not a text body (" + h.Get("Content-Type") + ")"
	}
	if enc := strings.TrimSpace(h.Get("Content-Encoding")); enc != "" && !strings.EqualFold(enc, "identity") {
		d, ok := decode(body, enc, editLimit)
		if !ok || d.truncated {
			return nil, "its Content-Encoding (" + enc + ") could not be decoded"
		}
		body = d.body
	}
	if !utf8.Valid(body) {
		return nil, "it is not UTF-8 text"
	}
	return body, ""
}

type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }
