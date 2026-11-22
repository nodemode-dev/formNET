//go:build darwin

package machine

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
)

const attributes = true

// lookupPort asks lsof for the TCP socket whose local end is ip:port. lsof
// lists both ends of a loopback connection (FormNET holds the other one),
// so the record whose local address carries the port and whose pid is not
// FormNET's own is the client. ps then gives the executable's name, as the
// snapshot does, since lsof's command is the process title. Without
// privilege lsof lists this user's processes only.
func lookupPort(ctx context.Context, ip net.IP, port int) (int, string, bool) {
	addr := ip.String()
	if ip.To4() == nil {
		addr = "[" + addr + "]"
	}
	out, err := exec.CommandContext(ctx, lsofPath, "-b", "-w", "-nP", "-iTCP@"+addr+":"+strconv.Itoa(port), "-sTCP:ESTABLISHED", "-Fpcn").Output()
	if err != nil && len(out) == 0 {
		return 0, "", false
	}
	pid, name := lsofClient(string(out), port, os.Getpid())
	if pid <= 0 {
		return 0, "", false
	}
	if raw, err := exec.CommandContext(ctx, psPath, "-o", "comm=", "-p", strconv.Itoa(pid)).Output(); err == nil {
		if comm := strings.TrimSpace(string(raw)); comm != "" {
			name = path.Base(comm)
		}
	}
	return pid, name, name != ""
}

// lsofClient reads `lsof -Fpcn` records: p (pid), c (command), n (local->remote).
func lsofClient(text string, port, self int) (int, string) {
	suffix := ":" + strconv.Itoa(port)
	pid, cmd := 0, ""
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			continue
		}
		switch line[0] {
		case 'p':
			pid, _ = strconv.Atoi(line[1:])
			cmd = ""
		case 'c':
			cmd = line[1:]
		case 'n':
			local, _, _ := strings.Cut(line[1:], "->")
			if pid > 0 && pid != self && strings.HasSuffix(local, suffix) {
				return pid, cmd
			}
		}
	}
	return 0, ""
}
