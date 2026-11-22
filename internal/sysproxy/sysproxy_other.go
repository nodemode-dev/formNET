//go:build !darwin && !windows

package sysproxy

import (
	"errors"
	"runtime"
)

func platformSupported() (bool, string) {
	where := "this system"
	if runtime.GOOS == "linux" {
		where = "Linux"
	}
	return false, "FormNET sets the system proxy on macOS and Windows; on " + where + ", point programs at the proxy with the lines on the Setup page"
}

var errUnsupported = errors.New("FormNET sets the system proxy on macOS and Windows only")

func readSystem() (snapshot, error) { return snapshot{OS: runtime.GOOS}, nil }

func pointsAt(snapshot, string) bool { return false }

func describe(snapshot) []Service { return []Service{} }

func autoConfig(snapshot) bool { return false }

func applySystem(string, snapshot) error { return errUnsupported }

func restoreSystem(snapshot, snapshot, string) error { return errUnsupported }
