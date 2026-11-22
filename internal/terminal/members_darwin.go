package terminal

import "golang.org/x/sys/unix"

func members(sid int) []int {
	procs, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil
	}
	out := []int{}
	for _, kp := range procs {
		pid := int(kp.Proc.P_pid)
		if got, err := unix.Getsid(pid); err == nil && got == sid {
			out = append(out, pid)
		}
	}
	return out
}
