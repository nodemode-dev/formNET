package mcp

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/nodemode-dev/formnet/internal/userfile"
)

const (
	exportsDir = "exports"
	maxExport  = 1 << 30
)

var reservedNames = []string{"CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9"}

// exportName turns the name a caller gave into a file name inside the
// exports folder, or says why it cannot be one.
func exportName(name string) (string, error) {
	if name == "" || strings.ContainsAny(name, `/\:`) || strings.Contains(name, "..") || filepath.Base(name) != name {
		return "", failf("file_name must be a plain file name such as session.har: no folders, separators or ..")
	}
	if !strings.HasSuffix(strings.ToLower(name), ".har") {
		name += ".har"
	}
	stem := strings.ToUpper(strings.SplitN(name, ".", 2)[0])
	if slices.Contains(reservedNames, stem) {
		return "", failf("%s is a reserved device name on Windows; choose another file_name", stem)
	}
	return name, nil
}

// exportFolder returns <data>/exports, making it private to the user when it
// is new and refusing a link or a file in its place.
func exportFolder(data string) (string, error) {
	dir := filepath.Join(data, exportsDir)
	fi, err := os.Lstat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			return "", err
		}
		fi, err = os.Lstat(dir)
	}
	if err != nil {
		return "", err
	}
	if fi.Mode()&fs.ModeSymlink != 0 || !fi.IsDir() {
		return "", failf("%s is not a plain folder; exports are written only into a real folder there", dir)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(dir, 0o700); err != nil {
			return "", err
		}
	}
	return dir, nil
}

func exportHAR(ctx context.Context, s *Server, a args) (any, error) {
	name, err := exportName(a.str("file_name"))
	if err != nil {
		return nil, err
	}
	r, err := s.locate()
	if err != nil {
		return nil, err
	}
	dir, err := exportFolder(s.Data)
	if err != nil {
		return nil, err
	}
	target := filepath.Join(dir, name)
	if filepath.Dir(target) != dir {
		return nil, failf("file_name must stay inside %s", dir)
	}
	limit, _ := a.num("limit")
	q := flowQuery(a, limit)
	secrets, _ := a.flag("include_secrets")
	if secrets {
		q.Set("secrets", "1")
	}
	resp, err := s.send(ctx, r, "GET", "/api/export/har", q, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	f, err := userfile.Create(target)
	if errors.Is(err, fs.ErrExist) {
		return nil, failf("%s already exists in %s; exports never replace a file, so choose another file_name", name, dir)
	}
	if err != nil {
		return nil, err
	}
	n, err := io.Copy(f, io.LimitReader(resp.Body, maxExport+1))
	if err == nil && n > maxExport {
		err = failf("the export is over %d MB; narrow it with the filters or limit", maxExport>>20)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(target)
		return nil, err
	}
	note := "Credential headers (Authorization, Proxy-Authorization, Cookie, Set-Cookie) read [redacted]; query strings and bodies are as captured. Keep the file on this computer."
	if secrets {
		note = "This file holds every header as captured, including credentials. Keep it on this computer and delete it when done."
	}
	return struct {
		Path    string `json:"path"`
		File    string `json:"file_name"`
		Bytes   int64  `json:"bytes"`
		Secrets bool   `json:"secrets_included"`
		Note    string `json:"note"`
	}{target, name, n, secrets, note}, nil
}

func clearFlows(ctx context.Context, s *Server, _ args) (any, error) {
	var before struct {
		Total int `json:"total"`
	}
	q := map[string][]string{"limit": {"1"}}
	if _, err := s.call(ctx, "GET", "/api/flows", q, nil, &before); err != nil {
		return nil, err
	}
	if _, err := s.call(ctx, "POST", "/api/flows/clear", nil, map[string]any{}, nil); err != nil {
		return nil, err
	}
	return struct {
		Cleared bool `json:"cleared"`
		Removed int  `json:"removed"`
	}{true, before.Total}, nil
}

func setInterception(ctx context.Context, s *Server, a args) (any, error) {
	if _, err := s.call(ctx, "POST", "/api/proxy", nil, map[string]any{"intercept": a.str("mode")}, nil); err != nil {
		return nil, err
	}
	return proxyRead(ctx, s)
}

func setPassthrough(ctx context.Context, s *Server, a args) (any, error) {
	patterns, _ := a.list("patterns")
	if _, err := s.call(ctx, "POST", "/api/proxy", nil, map[string]any{"passthrough": patterns}, nil); err != nil {
		return nil, err
	}
	return proxyRead(ctx, s)
}

func writeTools() []*Tool {
	limitLo, limitHi := intRange(1, 100000)
	return []*Tool{
		{
			Name:  "export_har",
			Title: "Export HAR file",
			Description: "Writes a file. Saves the matching flows as a HAR 1.2 file under FormNET's data folder, in <data>/exports only, and returns its path; it never writes anywhere else and never replaces an existing file. " +
				"Use it when the person wants a recording to open in a browser's developer tools or another HAR viewer. file_name is a plain name (letters, digits, dot, dash, underscore; .har is added), never a path. " +
				"Credential headers are redacted by default. include_secrets=true writes them as captured: that is an approval choice, so ask the person before setting it. Tunnels and refused flows have no HTTP shape and are left out.",
			Class:      Confined,
			Idempotent: false,
			Props: append([]prop{
				{Name: "file_name", Type: "string", Pattern: `^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`, MaxLength: 100, Description: "Plain file name for the export, such as checkout-bug.har. No folders, separators or .."},
				{Name: "include_secrets", Type: "boolean", Default: false, Description: "Needs the person's approval. true writes Authorization, Proxy-Authorization, Cookie and Set-Cookie values as captured; false (the default) writes [redacted]."},
				{Name: "limit", Type: "integer", Min: limitLo, Max: limitHi, Description: "Only the newest this many matching flows; leave out for every match."},
			}, filterProps()...),
			Required: []string{"file_name"},
			Output: obj(map[string]any{
				"path": typ("string", "Where the file was written, inside <data>/exports."), "file_name": typ("string", "The file name used."), "bytes": typ("integer", "Size of the file."),
				"secrets_included": typ("boolean", "True when credential headers are in the file."), "note": typ("string", "What the file holds and how to keep it."),
			}, "path", "file_name", "bytes", "secrets_included", "note"),
			Run: exportHAR,
		},
		{
			Name:  "clear_flows",
			Title: "Clear the recording",
			Description: "Needs approval: destructive. Forgets every flow FormNET holds, for every program, in the web client too; it cannot be undone. " +
				"Use it only when the person asks to start a clean recording. Export first with export_har if anything may be needed later.",
			Class:       Approval,
			Destructive: true,
			Idempotent:  true,
			Output:      obj(map[string]any{"cleared": typ("boolean", "True once the recording is empty."), "removed": typ("integer", "Flows held just before clearing.")}, "cleared", "removed"),
			Run:         clearFlows,
		},
		{
			Name:  "set_interception",
			Title: "Set HTTPS interception",
			Description: "Needs approval: changes the proxy for every program using it, and is kept for FormNET's next start. Sets whether HTTPS is opened: auto opens it while this computer trusts the root, on opens it always, off tunnels it and only counts hosts and bytes. " +
				"Use it when the person wants HTTPS contents recorded or wants interception stopped. With on and an untrusted root, programs that check certificates refuse the proxy. Returns the proxy status read back.",
			Class:       Approval,
			Destructive: true,
			Idempotent:  true,
			Props:       []prop{{Name: "mode", Type: "string", Enum: []string{"auto", "on", "off"}, Description: "auto, on or off."}},
			Required:    []string{"mode"},
			Output:      proxySchema,
			Run:         setInterception,
		},
		{
			Name:  "set_passthrough",
			Title: "Set passthrough hosts",
			Description: "Needs approval: changes the proxy for every program using it, and is kept for FormNET's next start. Replaces the whole list of hosts whose HTTPS is tunnelled without interception. " +
				"Use it for programs that pin certificates (their flows show kind refused): add their hosts so they work through the proxy again, counted but not opened. Read the current list with proxy_status first and send it back with the change. Returns the proxy status read back.",
			Class:       Approval,
			Destructive: true,
			Idempotent:  true,
			Props: []prop{{Name: "patterns", Type: "array", MaxItems: 200, MaxLength: 253, Pattern: `^(\*\.)?[A-Za-z0-9._\[\]:-]+$`,
				Description: "The complete new list: host names such as api.example.com or *.suffix patterns such as *.apple.com. An empty list turns passthrough off."}},
			Required: []string{"patterns"},
			Output:   proxySchema,
			Run:      setPassthrough,
		},
	}
}
