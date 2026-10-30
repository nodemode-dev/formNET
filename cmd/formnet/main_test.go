package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nodemode-dev/formnet/internal/config"
)

func TestVersionHelpAndUnknown(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"version"}, &out, &errOut); code != 0 || strings.TrimSpace(out.String()) == "" {
		t.Fatalf("version: %d %q", code, out.String())
	}
	out.Reset()
	if code := run([]string{"help"}, &out, &errOut); code != 0 || !strings.Contains(out.String(), "FORMNET_PROXY_ADDR") {
		t.Fatalf("help: %d", code)
	}
	if code := run([]string{"bogus"}, &out, &errOut); code != 2 || !strings.Contains(errOut.String(), "unknown command") {
		t.Fatalf("unknown: %d %q", code, errOut.String())
	}
}

func TestSessionTokenIsKeptPrivately(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{Data: dir, Token: "0123456789abcdef0123456789abcdef"}
	got, err := sessionToken(cfg)
	if err != nil || got != cfg.Token {
		t.Fatalf("configured token: %q %v", got, err)
	}
	info, err := os.Stat(filepath.Join(dir, tokenFile))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("token file: %v %v", info, err)
	}
	cfg.Token = ""
	made, err := sessionToken(cfg)
	if err != nil || len(made) != 48 || made == got {
		t.Fatalf("made token: %q %v", made, err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, tokenFile))
	if strings.TrimSpace(string(raw)) != made {
		t.Fatalf("file holds %q, want %q", raw, made)
	}
	if u := sessionURL("127.0.0.1:8760", made); u != "http://127.0.0.1:8760/?token="+made {
		t.Fatalf("url %q", u)
	}
}
