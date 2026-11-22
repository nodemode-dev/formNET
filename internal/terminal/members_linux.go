package terminal

import (
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)

func members(sid int) []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	out := []int{}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		if got, err := unix.Getsid(pid); err == nil && got == sid {
			out = append(out, pid)
		}
	}
	return out
}
