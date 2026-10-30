package machine

import (
	"net"
	"sort"
	"strings"
	"time"
)

// procInfo is what a platform knows about one process beyond its connections.
type procInfo struct {
	name     string
	path     string
	user     string
	bytesIn  int64
	bytesOut int64
}

// joinAddr renders host and port as ip:port, bracketing IPv6.
func joinAddr(host, port string) string {
	return net.JoinHostPort(host, port)
}

// wildcardHost maps a tool's "*" (any address) to the family's unspecified address.
func wildcardHost(host string, ipv6 bool) string {
	if host != "*" {
		return host
	}
	if ipv6 {
		return "::"
	}
	return "0.0.0.0"
}

// protoFor normalises a protocol name and address family to tcp, tcp6, udp or udp6.
func protoFor(proto string, ipv6 bool) string {
	p := strings.ToLower(strings.TrimSpace(proto))
	switch {
	case strings.HasPrefix(p, "tcp"):
		p = "tcp"
	case strings.HasPrefix(p, "udp"):
		p = "udp"
	}
	if ipv6 {
		return p + "6"
	}
	return p
}

// defaultState fills the state a tool leaves blank: UDP sockets have none.
func defaultState(proto, state string) string {
	s := strings.ToUpper(strings.TrimSpace(state))
	if s == "LISTENING" {
		return "LISTEN"
	}
	if s == "" && strings.HasPrefix(proto, "udp") {
		return "UDP"
	}
	return s
}

// buildProcesses aggregates connections per pid, naming each from info.
// Sockets whose owner is hidden from this user (pid 0) stay in the
// connection list but make no process.
func buildProcesses(conns []Connection, info map[int]procInfo) []Process {
	byPID := map[int]*Process{}
	for _, c := range conns {
		if c.PID == 0 {
			continue
		}
		p, ok := byPID[c.PID]
		if !ok {
			pi := info[c.PID]
			name := pi.name
			if name == "" {
				name = c.Process
			}
			p = &Process{PID: c.PID, Name: name, Path: pi.path, User: pi.user, BytesIn: pi.bytesIn, BytesOut: pi.bytesOut}
			byPID[c.PID] = p
		}
		p.Connections++
	}
	out := make([]Process, 0, len(byPID))
	for _, p := range byPID {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Connections != out[j].Connections {
			return out[i].Connections > out[j].Connections
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].PID < out[j].PID
	})
	return out
}

// sortConnections orders by process name, then remote, then local.
func sortConnections(conns []Connection) []Connection {
	out := make([]Connection, len(conns))
	copy(out, conns)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Process != out[j].Process {
			return out[i].Process < out[j].Process
		}
		if out[i].Remote != out[j].Remote {
			return out[i].Remote < out[j].Remote
		}
		if out[i].Local != out[j].Local {
			return out[i].Local < out[j].Local
		}
		return out[i].Proto < out[j].Proto
	})
	return out
}

// assemble builds the snapshot a reader returns.
func assemble(source string, conns []Connection, info map[int]procInfo) Snapshot {
	taken := time.Now()
	named := make([]Connection, 0, len(conns))
	for _, c := range conns {
		if c.Process == "" {
			c.Process = info[c.PID].name
		}
		if c.Path == "" {
			c.Path = info[c.PID].path
		}
		c.FirstSeen = taken
		c.LastSeen = taken
		named = append(named, c)
	}
	return Snapshot{
		Taken:       taken,
		Source:      source,
		Supported:   true,
		Processes:   buildProcesses(named, info),
		Connections: sortConnections(named),
	}
}
