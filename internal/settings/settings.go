// Package settings keeps the proxy choices a person makes in the client
// (interception, the passthrough list, the system proxy) across restarts, in
// <data>/settings.json, readable by that user only.
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/nodemode-dev/formnet/internal/userfile"
)

// FileName is the settings file inside the data folder.
const FileName = "settings.json"

// Version is the schema version this build writes.
const Version = 1

// Settings is the file's content.
type Settings struct {
	Version     int      `json:"version"`
	Intercept   string   `json:"intercept"` // auto, on or off
	Passthrough []string `json:"passthrough"`
	SystemProxy bool     `json:"system_proxy"` // point the system proxy at FormNET while it runs; absent in older files means false
}

// Defaults is what a first start uses.
func Defaults() Settings {
	return Settings{Version: Version, Intercept: "auto", Passthrough: []string{}}
}

// File is the settings file of one data folder. It holds what the file says,
// never what an environment variable chose for one run.
type File struct {
	path string
	mu   sync.Mutex
	cur  Settings
}

// Open reads <dir>/settings.json. A missing file gives the defaults; a file
// that does not parse gives the defaults and the error, and is replaced only
// by the next change.
func Open(dir string) (*File, error) {
	f := &File{path: filepath.Join(dir, FileName), cur: Defaults()}
	raw, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return f, err
	}
	var s Settings
	if err := json.Unmarshal(raw, &s); err != nil {
		return f, fmt.Errorf("%s: %w", f.path, err)
	}
	switch s.Intercept = strings.ToLower(strings.TrimSpace(s.Intercept)); s.Intercept {
	case "auto", "on", "off":
	default:
		s.Intercept = "auto"
	}
	s.Passthrough = clean(s.Passthrough)
	s.Version = Version
	f.cur = s
	return f, nil
}

// Current returns what the file says.
func (f *File) Current() Settings {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.cur
	out.Passthrough = append([]string{}, f.cur.Passthrough...)
	return out
}

// Update changes the fields given (nil leaves one as it is) and writes the whole file.
func (f *File) Update(intercept *string, passthrough []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	next := f.cur
	if intercept != nil {
		next.Intercept = *intercept
	}
	if passthrough != nil {
		next.Passthrough = clean(passthrough)
	}
	return f.writeLocked(next)
}

// SetSystemProxy keeps the system proxy choice and writes the whole file.
func (f *File) SetSystemProxy(on bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	next := f.cur
	next.SystemProxy = on
	return f.writeLocked(next)
}

func (f *File) writeLocked(next Settings) error {
	next.Version = Version
	raw, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	if err := userfile.Write(f.path, append(raw, '\n')); err != nil {
		return err
	}
	f.cur = next
	return nil
}

// clean keeps host names and *.suffix patterns, lower case.
func clean(patterns []string) []string {
	out := make([]string, 0, len(patterns))
	for _, p := range patterns {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" || strings.ContainsAny(p, " /\\") {
			continue
		}
		out = append(out, p)
	}
	return out
}
