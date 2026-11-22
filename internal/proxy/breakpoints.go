package proxy

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/nodemode-dev/formnet/internal/flow"
)

const (
	// DefaultBreakpointTimeout releases a pause nobody answered.
	DefaultBreakpointTimeout = 60 * time.Second
	// DefaultMaxPaused is how many exchanges may wait at once.
	DefaultMaxPaused = 20
)

// Breakpoint stages.
const (
	StageRequest  = "request"
	StageResponse = "response"
)

// ErrNoBreakpoint: nothing is paused under that id (it was answered, timed out or its client left).
var ErrNoBreakpoint = errors.New("nothing is paused under that id: it was answered, released after its timeout, or its client went away")

// Paused is one exchange waiting at a breakpoint, as the API shows it.
type Paused struct {
	ID       string        `json:"id"`
	FlowID   string        `json:"flow_id"`
	Stage    string        `json:"stage"` // request or response
	Rule     string        `json:"rule"`
	Since    time.Time     `json:"since"`
	Deadline time.Time     `json:"deadline"`
	Method   string        `json:"method,omitempty"` // request stage
	URL      string        `json:"url"`
	Status   int           `json:"status,omitempty"` // response stage
	Headers  []flow.Header `json:"headers"`
	// The body as it would go out: text when it is UTF-8, else base64.
	// body_decoded says the text is the decoded form of the Content-Encoding;
	// an edited body is then sent without that header.
	BodyText    *string `json:"body_text,omitempty"`
	BodyBase64  *string `json:"body_base64,omitempty"`
	BodyBytes   int     `json:"body_bytes"`
	BodyDecoded bool    `json:"body_decoded"`
}

// BreakpointEdit changes a paused message before it goes on; a nil field keeps it.
type BreakpointEdit struct {
	Method  *string
	URL     *string
	Status  *int
	Headers *[]flow.Header
	Body    *[]byte
}

func (e BreakpointEdit) empty() bool {
	return e.Method == nil && e.URL == nil && e.Status == nil && e.Headers == nil && e.Body == nil
}

// BreakpointEvent is what the event stream carries about pauses.
type BreakpointEvent struct {
	Type     string     `json:"type"` // breakpoint.paused or breakpoint.resumed
	ID       string     `json:"id"`
	FlowID   string     `json:"flow_id"`
	Stage    string     `json:"stage"`
	Rule     string     `json:"rule"`
	Deadline *time.Time `json:"deadline,omitempty"` // paused only
	Outcome  string     `json:"outcome,omitempty"`  // resumed: continued, edited, aborted, timeout, client_gone
}

// BreakpointsStatus is what GET /api/breakpoints answers.
type BreakpointsStatus struct {
	Paused         []Paused `json:"paused"`
	TimeoutSeconds int      `json:"timeout_seconds"`
	MaxPaused      int      `json:"max_paused"`
}

type outcome int

const (
	outcomeContinue outcome = iota
	outcomeAbort
	outcomeTimeout
	outcomeFull
	outcomeGone
)

type decision struct {
	abort bool
	edit  BreakpointEdit
}

type pauseEntry struct {
	info   Paused
	decide chan decision
}

type breakpoints struct {
	mu      sync.Mutex
	timeout time.Duration
	max     int
	paused  map[string]*pauseEntry
	order   []string
	subs    map[chan BreakpointEvent]struct{}
}

func newBreakpoints(timeout time.Duration, max int) *breakpoints {
	if timeout <= 0 {
		timeout = DefaultBreakpointTimeout
	}
	if max <= 0 {
		max = DefaultMaxPaused
	}
	return &breakpoints{timeout: timeout, max: max, paused: map[string]*pauseEntry{}, subs: map[chan BreakpointEvent]struct{}{}}
}

// pause holds the caller until the page answers, the timeout passes or ctx
// ends. With max exchanges already waiting it returns outcomeFull at once.
func (b *breakpoints) pause(ctx context.Context, info Paused) (decision, outcome) {
	b.mu.Lock()
	if len(b.paused) >= b.max {
		b.mu.Unlock()
		return decision{}, outcomeFull
	}
	info.ID = newID()
	info.Since = time.Now()
	info.Deadline = info.Since.Add(b.timeout)
	e := &pauseEntry{info: info, decide: make(chan decision, 1)}
	b.paused[info.ID] = e
	b.order = append(b.order, info.ID)
	timeout := b.timeout
	b.publishLocked(BreakpointEvent{Type: "breakpoint.paused", ID: info.ID, FlowID: info.FlowID, Stage: info.Stage, Rule: info.Rule, Deadline: &info.Deadline})
	b.mu.Unlock()

	t := time.NewTimer(timeout)
	defer t.Stop()
	var d decision
	var out outcome
	select {
	case d = <-e.decide:
		out = outcomeContinue
		if d.abort {
			out = outcomeAbort
		}
	case <-t.C:
		out = outcomeTimeout
	case <-ctx.Done():
		out = outcomeGone
	}
	b.mu.Lock()
	if _, still := b.paused[info.ID]; still {
		b.removeLocked(info.ID)
	} else if out != outcomeContinue && out != outcomeAbort {
		// Answered in the same instant the wait ended: take the answer.
		select {
		case d = <-e.decide:
			out = outcomeContinue
			if d.abort {
				out = outcomeAbort
			}
		default:
		}
	}
	name := map[outcome]string{outcomeContinue: "continued", outcomeAbort: "aborted", outcomeTimeout: "timeout", outcomeGone: "client_gone"}[out]
	if out == outcomeContinue && !d.edit.empty() {
		name = "edited"
	}
	b.publishLocked(BreakpointEvent{Type: "breakpoint.resumed", ID: info.ID, FlowID: info.FlowID, Stage: info.Stage, Rule: info.Rule, Outcome: name})
	b.mu.Unlock()
	return d, out
}

func (b *breakpoints) removeLocked(id string) {
	delete(b.paused, id)
	for i, x := range b.order {
		if x == id {
			b.order = append(b.order[:i:i], b.order[i+1:]...)
			break
		}
	}
}

func (b *breakpoints) publishLocked(e BreakpointEvent) {
	for ch := range b.subs {
		select {
		case ch <- e:
		default:
		}
	}
}

func (b *breakpoints) answer(id string, d decision) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.paused[id]
	if !ok {
		return ErrNoBreakpoint
	}
	if !d.abort {
		if err := checkEdit(e.info.Stage, d.edit); err != nil {
			return err
		}
	}
	b.removeLocked(id)
	e.decide <- d
	return nil
}

func (b *breakpoints) status() BreakpointsStatus {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := BreakpointsStatus{Paused: make([]Paused, 0, len(b.order)), TimeoutSeconds: int(b.timeout / time.Second), MaxPaused: b.max}
	for _, id := range b.order {
		out.Paused = append(out.Paused, b.paused[id].info)
	}
	return out
}

// checkEdit refuses an edit that cannot go out.
func checkEdit(stage string, e BreakpointEdit) error {
	if stage == StageRequest {
		if e.Status != nil {
			return badInput("status is edited at a response breakpoint; this one holds a request")
		}
		if e.Method != nil {
			m := strings.ToUpper(strings.TrimSpace(*e.Method))
			if !token(m) || m == http.MethodConnect {
				return badInput("method must be an HTTP method such as GET or POST")
			}
		}
		if e.URL != nil {
			u, err := url.Parse(strings.TrimSpace(*e.URL))
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
				return badInput("url must be an absolute http or https URL")
			}
		}
	} else {
		if e.Method != nil || e.URL != nil {
			return badInput("method and url are edited at a request breakpoint; this one holds a response")
		}
		if e.Status != nil && (*e.Status < 100 || *e.Status > 999) {
			return badInput("status must be an HTTP status between 100 and 999")
		}
	}
	if e.Headers != nil {
		for _, h := range *e.Headers {
			if !token(strings.TrimSpace(h.Name)) {
				return badInput("header name " + strconv.Quote(h.Name) + " is not valid")
			}
			if strings.ContainsAny(h.Value, "\r\n\x00") {
				return badInput("the value of header " + h.Name + " holds a line break")
			}
		}
	}
	if e.Body != nil && len(*e.Body) > editLimit {
		return badInput("an edited body may be at most 8 MiB")
	}
	return nil
}

func (p *Proxy) breakpointsOf() (*breakpoints, error) {
	st := p.ext.Load()
	if st == nil {
		return nil, ErrTrafficUnavailable
	}
	return st.bp, nil
}

// Breakpoints lists the paused exchanges and the pause settings.
func (p *Proxy) Breakpoints() BreakpointsStatus {
	b, err := p.breakpointsOf()
	if err != nil {
		return BreakpointsStatus{Paused: []Paused{}}
	}
	return b.status()
}

// SetBreakpointSettings changes the timeout (seconds, 1 to 3600) and the most paused at once (1 to 200); nil keeps one.
func (p *Proxy) SetBreakpointSettings(timeoutSeconds, maxPaused *int) (BreakpointsStatus, error) {
	b, err := p.breakpointsOf()
	if err != nil {
		return BreakpointsStatus{}, err
	}
	if timeoutSeconds != nil && (*timeoutSeconds < 1 || *timeoutSeconds > 3600) {
		return BreakpointsStatus{}, badInput("timeout_seconds must be between 1 and 3600")
	}
	if maxPaused != nil && (*maxPaused < 1 || *maxPaused > 200) {
		return BreakpointsStatus{}, badInput("max_paused must be between 1 and 200")
	}
	b.mu.Lock()
	if timeoutSeconds != nil {
		b.timeout = time.Duration(*timeoutSeconds) * time.Second
	}
	if maxPaused != nil {
		b.max = *maxPaused
	}
	t, m := int(b.timeout/time.Second), b.max
	b.mu.Unlock()
	if st := p.ext.Load(); st.opts.RememberBreakpoints != nil {
		if err := st.opts.RememberBreakpoints(t, m); err != nil {
			p.log.Warn("settings file", "err", err)
		}
	}
	return b.status(), nil
}

// ContinueBreakpoint lets a paused exchange go on, with an optional edit.
func (p *Proxy) ContinueBreakpoint(id string, edit BreakpointEdit) error {
	b, err := p.breakpointsOf()
	if err != nil {
		return err
	}
	return b.answer(id, decision{edit: edit})
}

// AbortBreakpoint ends a paused exchange: the client's connection is closed.
func (p *Proxy) AbortBreakpoint(id string) error {
	b, err := p.breakpointsOf()
	if err != nil {
		return err
	}
	return b.answer(id, decision{abort: true})
}

// SubscribeBreakpoints delivers pause events until ctx ends.
func (p *Proxy) SubscribeBreakpoints(ctx context.Context) <-chan BreakpointEvent {
	ch := make(chan BreakpointEvent, 64)
	b, err := p.breakpointsOf()
	if err != nil {
		return nil
	}
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	go func() {
		<-ctx.Done()
		b.mu.Lock()
		delete(b.subs, ch)
		b.mu.Unlock()
	}()
	return ch
}

// showBody fills the body fields of a paused message.
func showBody(info *Paused, body []byte, decoded bool) {
	info.BodyBytes = len(body)
	info.BodyDecoded = decoded
	if len(body) == 0 {
		return
	}
	if utf8.Valid(body) {
		s := string(body)
		info.BodyText = &s
		return
	}
	s := base64.StdEncoding.EncodeToString(body)
	info.BodyBase64 = &s
}
