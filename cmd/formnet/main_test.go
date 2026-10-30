package main

import (
	"bytes"
	"strings"
	"testing"
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
