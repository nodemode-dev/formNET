package machine

import (
	"path"
	"strconv"
	"strings"
)

// parsePS reads `ps -axo pid=,user=,comm=`: pid, user, then the command path,
// which may contain spaces. lsof's c field is the process title (Claude Code
// shows as its version number), so ps supplies the executable and its base
// name instead.
func parsePS(text string) map[int]procInfo {
	out := map[int]procInfo{}
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		trimmed := strings.TrimSpace(line)
		rest := strings.TrimSpace(strings.TrimPrefix(trimmed, fields[0]))
		comm := strings.TrimSpace(strings.TrimPrefix(rest, fields[1]))
		if comm == "" {
			continue
		}
		out[pid] = procInfo{user: fields[1], path: comm, name: path.Base(comm)}
	}
	return out
}

// withPS returns a copy of info where every process ps knows gets its
// executable path and the executable's base name; processes without a ps
// row keep what lsof said.
func withPS(info map[int]procInfo, ps map[int]procInfo) map[int]procInfo {
	out := make(map[int]procInfo, len(info))
	for pid, pi := range info {
		if row, ok := ps[pid]; ok {
			pi.path = row.path
			if row.name != "" {
				pi.name = row.name
			}
			if pi.user == "" {
				pi.user = row.user
			}
		}
		out[pid] = pi
	}
	return out
}

// renamed returns a copy of conns whose Process names follow info.
func renamed(conns []Connection, info map[int]procInfo) []Connection {
	out := make([]Connection, len(conns))
	for i, c := range conns {
		if name := info[c.PID].name; name != "" {
			c.Process = name
		}
		out[i] = c
	}
	return out
}
