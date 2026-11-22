package settings

// Throttle is the throttle choice; rates in kbit/s.
type Throttle struct {
	Enabled   bool     `json:"enabled"`
	Preset    string   `json:"preset"`
	DownKbps  int      `json:"down_kbps"`
	UpKbps    int      `json:"up_kbps"`
	LatencyMS int      `json:"latency_ms"`
	Hosts     []string `json:"hosts"`
}

// Remote is capture from other devices: the switch and the allow list.
type Remote struct {
	Enabled bool     `json:"enabled"`
	Allow   []string `json:"allow"`
}

// Breakpoints are the pause settings.
type Breakpoints struct {
	TimeoutSeconds int `json:"timeout_seconds"`
	MaxPaused      int `json:"max_paused"`
}

// SetThrottle keeps the throttle choice and writes the whole file.
func (f *File) SetThrottle(t Throttle) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	next := f.cur
	t.Hosts = append([]string{}, t.Hosts...)
	next.Throttle = &t
	return f.writeLocked(next)
}

// SetRemote keeps the remote capture choice and writes the whole file.
func (f *File) SetRemote(enabled bool, allow []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	next := f.cur
	next.Remote = &Remote{Enabled: enabled, Allow: append([]string{}, allow...)}
	return f.writeLocked(next)
}

// SetBreakpoints keeps the pause settings and writes the whole file.
func (f *File) SetBreakpoints(timeoutSeconds, maxPaused int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	next := f.cur
	next.Breakpoints = &Breakpoints{TimeoutSeconds: timeoutSeconds, MaxPaused: maxPaused}
	return f.writeLocked(next)
}
