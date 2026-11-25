// Package sysproxy points the system's proxy settings at FormNET while it
// runs, as a choice, and puts the previous settings back when it stops.
//
// On macOS it changes the web and secure web proxy of every enabled network
// service through /usr/sbin/networksetup; on Windows the current user's
// WinINET settings in the registry. Other systems are reported as not
// supported. Before anything changes, the previous settings are written to
// <data>/sysproxy.json (the user's alone), so a run that dies can be undone
// by the next start or by `formnet sysproxy restore`.
package sysproxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/nodemode-dev/formnet/internal/userfile"
)

// FileName is the record of the settings FormNET replaced, inside the data folder.
const FileName = "sysproxy.json"

// Service is one place the system keeps a proxy setting: a macOS network
// service, or the Windows user's settings. Web and Secure are host:port while
// that proxy is on, empty while it is off.
type Service struct {
	Name   string `json:"name"`
	Web    string `json:"web"`
	Secure string `json:"secure"`
}

// Status is what the API and the CLI report.
type Status struct {
	Supported  bool      `json:"supported"`
	Reason     string    `json:"reason"`
	Enabled    bool      `json:"enabled"` // the saved choice
	Active     bool      `json:"active"`  // the system points at FormNET now
	Address    string    `json:"address"`
	Services   []Service `json:"services"`
	AutoConfig bool      `json:"auto_config"`
	Error      string    `json:"error"`
}

// endpoint is one proxy setting as the system holds it.
type endpoint struct {
	Enabled bool   `json:"enabled"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Auth    bool   `json:"auth,omitempty"`
}

// serviceState is one macOS network service's settings.
type serviceState struct {
	Name         string   `json:"name"`
	Web          endpoint `json:"web"`
	Secure       endpoint `json:"secure"`
	Bypass       []string `json:"bypass"`
	AutoProxy    bool     `json:"auto_proxy"`
	AutoProxyURL string   `json:"auto_proxy_url,omitempty"`
}

// wininetState is the Windows user's settings; a nil field was not set.
type wininetState struct {
	ProxyEnable   *uint32 `json:"proxy_enable"`
	ProxyServer   *string `json:"proxy_server"`
	ProxyOverride *string `json:"proxy_override"`
	AutoConfigURL string  `json:"auto_config_url,omitempty"`
}

// snapshot is the system's proxy settings at one moment.
type snapshot struct {
	OS       string         `json:"os"`
	Services []serviceState `json:"services,omitempty"`
	WinINET  *wininetState  `json:"wininet,omitempty"`
}

// record is the content of sysproxy.json.
type record struct {
	Version  int       `json:"version"`
	Address  string    `json:"address"` // the FormNET proxy the system was pointed at
	API      string    `json:"api"`     // that run's API, to tell whether it still runs
	PID      int       `json:"pid"`
	Set      time.Time `json:"set"`
	Previous snapshot  `json:"previous"`
}

const recordVersion = 1

func recordPath(data string) string { return filepath.Join(data, FileName) }

func readRecord(data string) (record, bool, error) {
	raw, err := os.ReadFile(recordPath(data))
	if errors.Is(err, os.ErrNotExist) {
		return record{}, false, nil
	}
	if err != nil {
		return record{}, false, err
	}
	var r record
	if err := json.Unmarshal(raw, &r); err != nil {
		return record{}, true, fmt.Errorf("%s: %w", recordPath(data), err)
	}
	return r, true, nil
}

func writeRecord(data string, r record) error {
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return userfile.Write(recordPath(data), append(raw, '\n'))
}

func removeRecord(data string) error {
	err := os.Remove(recordPath(data))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Supported reports whether this system's proxy can be set, and why not.
func Supported() (bool, string) { return platformSupported() }

var probeClient = &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}

// alive reports whether a FormNET answers on api.
func alive(api string) bool {
	if api == "" {
		return false
	}
	resp, err := probeClient.Get("http://" + api + "/healthz")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var h struct {
		Product string `json:"product"`
	}
	return resp.StatusCode == http.StatusOK && json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&h) == nil && h.Product == "formnet"
}

func splitAddr(addr string) (string, int, error) {
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, err
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port > 65535 {
		return "", 0, fmt.Errorf("port %q", portText)
	}
	return host, port, nil
}

func (e endpoint) is(addr string) bool {
	host, port, err := splitAddr(addr)
	return err == nil && e.Enabled && e.Host == host && e.Port == port
}

func (e endpoint) String() string {
	if !e.Enabled || e.Host == "" {
		return ""
	}
	return net.JoinHostPort(e.Host, strconv.Itoa(e.Port))
}

// Recover undoes what a run that did not stop cleanly left: when sysproxy.json
// exists, its run no longer answers, and the system still points at the
// address it recorded, the previous settings go back; when the person has
// changed the settings since, they are left alone. The file is removed in both
// cases. It returns what it did, for the log, or nothing when there was nothing to do.
func Recover(data string) (string, error) {
	rec, found, err := readRecord(data)
	if !found {
		return "", err
	}
	if err != nil {
		_ = removeRecord(data)
		return "", fmt.Errorf("the record of the previous system proxy could not be read and was removed: %w", err)
	}
	if rec.PID == os.Getpid() {
		return "", nil
	}
	if alive(rec.API) {
		return "", nil
	}
	if ok, _ := platformSupported(); !ok {
		return "", removeRecord(data)
	}
	cur, err := readSystem()
	if err != nil {
		return "", fmt.Errorf("reading the system proxy: %w", err)
	}
	if !pointsAt(cur, rec.Address) {
		if err := removeRecord(data); err != nil {
			return "", err
		}
		return "the system proxy was changed since the last FormNET run set it, so it was left as it is", nil
	}
	if err := restoreSystem(rec.Previous, cur, rec.Address); err != nil {
		return "", err
	}
	if err := removeRecord(data); err != nil {
		return "", err
	}
	return "the last FormNET run did not stop cleanly; the system proxy it set is back to what it was before", nil
}

// Options configure a Manager.
type Options struct {
	Data     string
	Address  string // the proxy address this run listens on
	API      string // the API address of this run
	Enabled  bool   // the saved choice
	Remember func(on bool) error
	Log      *slog.Logger
}

// Manager sets and restores the system proxy for one run.
type Manager struct {
	opts Options

	mu      sync.Mutex
	enabled bool
	prev    *snapshot
	lastErr string
	cached  *snapshot
	readErr error
	readAt  time.Time
}

// New prepares a Manager; it changes nothing until Enable or Set.
func New(opts Options) *Manager {
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	return &Manager{opts: opts, enabled: opts.Enabled}
}

const cacheFor = 2 * time.Second

// Status reads the system (at most every two seconds) and reports.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statusLocked()
}

func (m *Manager) statusLocked() Status {
	st := Status{Enabled: m.enabled, Address: m.opts.Address, Services: []Service{}, Error: m.lastErr}
	st.Supported, st.Reason = platformSupported()
	if !st.Supported {
		return st
	}
	if m.cached == nil || time.Since(m.readAt) > cacheFor {
		cur, err := readSystem()
		m.cached, m.readErr, m.readAt = &cur, err, time.Now()
	}
	if m.readErr != nil && st.Error == "" {
		st.Error = "reading the system proxy: " + m.readErr.Error()
	}
	st.Services = describe(*m.cached)
	st.AutoConfig = autoConfig(*m.cached)
	st.Active = pointsAt(*m.cached, m.opts.Address)
	if st.AutoConfig {
		st.Reason = "the system prefers its automatic proxy configuration, which is on; programs that follow it may not use FormNET"
	}
	return st
}

// Set applies the choice at once and keeps it. Turning on is kept only when
// it worked; turning off is always kept.
func (m *Manager) Set(on bool) Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ok, _ := platformSupported(); !ok {
		m.lastErr = "FormNET cannot set the system proxy on this system; nothing was changed"
		return m.statusLocked()
	}
	var err error
	if on {
		err = m.enableLocked()
	} else {
		err = m.disableLocked()
	}
	m.cached = nil
	if err != nil {
		m.lastErr = err.Error()
		m.opts.Log.Warn("system proxy", "on", on, "err", err)
	} else {
		m.lastErr = ""
	}
	if err == nil || !on {
		m.enabled = on
		if m.opts.Remember != nil {
			if rerr := m.opts.Remember(on); rerr != nil {
				m.opts.Log.Warn("settings file", "err", rerr)
			}
		}
	}
	st := m.statusLocked()
	if err != nil && on {
		st.Active = false
	}
	return st
}

// Enable points the system at this run's proxy without changing the saved choice.
func (m *Manager) Enable() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	err := m.enableLocked()
	m.cached = nil
	if err != nil {
		m.lastErr = err.Error()
	} else {
		m.lastErr = ""
	}
	return err
}

// Close puts back what this run changed. It is safe to call when nothing was.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	err := m.disableLocked()
	m.cached = nil
	return err
}

const handoverWait = 5 * time.Second

func (m *Manager) enableLocked() error {
	if ok, _ := platformSupported(); !ok {
		return errors.New("FormNET cannot set the system proxy on this system; nothing was changed")
	}
	cur, err := readSystem()
	if err != nil {
		return fmt.Errorf("reading the system proxy: %w", err)
	}
	if m.prev != nil {
		if pointsAt(cur, m.opts.Address) {
			return nil
		}
		m.prev = nil
		_ = removeRecord(m.opts.Data)
	}
	prev := cur
	rec, found, rerr := readRecord(m.opts.Data)
	if found && rerr == nil && rec.PID != os.Getpid() {
		deadline := time.Now().Add(handoverWait)
		for found && alive(rec.API) && time.Now().Before(deadline) {
			time.Sleep(200 * time.Millisecond)
			rec, found, rerr = readRecord(m.opts.Data)
		}
		if found && rerr == nil && rec.PID != os.Getpid() {
			if alive(rec.API) {
				return fmt.Errorf("another FormNET (pid %d, %s) has set the system proxy; turn it off there first", rec.PID, rec.API)
			}
			if cur, err = readSystem(); err != nil {
				return fmt.Errorf("reading the system proxy: %w", err)
			}
			prev = cur
			if pointsAt(cur, rec.Address) {
				prev = rec.Previous
			}
		} else if found || rerr != nil {
			if cur, err = readSystem(); err != nil {
				return fmt.Errorf("reading the system proxy: %w", err)
			}
			prev = cur
		}
	}
	if err := writeRecord(m.opts.Data, record{Version: recordVersion, Address: m.opts.Address, API: m.opts.API, PID: os.Getpid(), Set: time.Now().UTC(), Previous: prev}); err != nil {
		return fmt.Errorf("the previous system proxy could not be saved, so nothing was changed: %w", err)
	}
	if err := applySystem(m.opts.Address, cur); err != nil {
		_ = removeRecord(m.opts.Data)
		return err
	}
	m.prev = &prev
	m.opts.Log.Info("the system proxy points at FormNET", "addr", m.opts.Address)
	return nil
}

func (m *Manager) disableLocked() error {
	if m.prev == nil {
		return nil
	}
	cur, err := readSystem()
	if err != nil {
		return fmt.Errorf("reading the system proxy: %w", err)
	}
	if err := restoreSystem(*m.prev, cur, m.opts.Address); err != nil {
		return err
	}
	m.prev = nil
	m.opts.Log.Info("the system proxy is back to what it was before FormNET set it")
	return removeRecord(m.opts.Data)
}
