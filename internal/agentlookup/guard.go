package agentlookup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// StandInsVar names the folder a test's stand-in agent programs live in.
const StandInsVar = "FORMNET_AGENT_STAND_INS"

// Guarded are the program names a test binary never starts from anywhere else.
var Guarded = []string{"claude", "codex", "cursor-agent", "agent", "agy"}

// Guard refuses, inside a test binary only, to start a guarded program that
// does not live under the folder StandInsVar names. Every start of a found
// program in FormNET passes through it: the --version check and a terminal tab.
func Guard(path string) error {
	if !testing.Testing() {
		return nil
	}
	name := strings.ToLower(filepath.Base(path))
	for _, ending := range windowsEndings {
		name = strings.TrimSuffix(name, ending)
	}
	hit := false
	for _, g := range Guarded {
		hit = hit || name == g
	}
	if !hit {
		return nil
	}
	if dir := os.Getenv(StandInsVar); dir != "" {
		root, err1 := filepath.Abs(dir)
		file, err2 := filepath.Abs(path)
		if err1 == nil && err2 == nil {
			if rel, err := filepath.Rel(root, file); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel) {
				return nil
			}
		}
	}
	return fmt.Errorf("a test binary does not start %s: put a stand-in under the folder %s names", path, StandInsVar)
}
