package machine

import (
	"strconv"
	"strings"
)

// lsofFile is one socket as lsof -F prints it, before it becomes a Connection.
type lsofFile struct {
	proto string
	ipv6  bool
	addr  string
	state string
}

// parseLsof reads `lsof -nP -iTCP -iUDP -F pcLPntT` output. Each process
// starts with p (pid), c (command) and L (user); each socket with f, then
// t (IPv4 or IPv6), P (protocol), n (addresses) and T lines (TST=STATE).
func parseLsof(text string) ([]Connection, map[int]procInfo) {
	var conns []Connection
	info := map[int]procInfo{}
	pid := 0
	var cur *lsofFile
	flush := func() {
		if cur != nil {
			if c, ok := lsofConnection(pid, info[pid].name, *cur); ok {
				conns = append(conns, c)
			}
			cur = nil
		}
	}
	for _, line := range strings.Split(text, "\n") {
		if len(line) < 1 {
			continue
		}
		tag, value := line[0], line[1:]
		switch tag {
		case 'p':
			flush()
			pid, _ = strconv.Atoi(value)
			info[pid] = procInfo{}
		case 'c':
			pi := info[pid]
			pi.name = value
			info[pid] = pi
		case 'L':
			pi := info[pid]
			pi.user = value
			info[pid] = pi
		case 'f':
			flush()
			cur = &lsofFile{}
		case 't':
			if cur != nil {
				cur.ipv6 = value == "IPv6"
			}
		case 'P':
			if cur != nil {
				cur.proto = value
			}
		case 'n':
			if cur != nil {
				cur.addr = value
			}
		case 'T':
			if cur != nil {
				if st, ok := strings.CutPrefix(value, "ST="); ok {
					cur.state = st
				}
			}
		}
	}
	flush()
	return conns, info
}

// lsofConnection turns one socket into a Connection; unbound sockets (*:*) are left out.
func lsofConnection(pid int, name string, f lsofFile) (Connection, bool) {
	if f.proto == "" || f.addr == "" || f.addr == "*:*" {
		return Connection{}, false
	}
	local, remote, _ := strings.Cut(f.addr, "->")
	ipv6 := f.ipv6 || strings.HasPrefix(local, "[")
	l, ok := lsofAddr(local, ipv6)
	if !ok {
		return Connection{}, false
	}
	r := ""
	if remote != "" {
		if ra, ok := lsofAddr(remote, ipv6); ok {
			r = ra
		}
	}
	proto := protoFor(f.proto, ipv6)
	return Connection{Proto: proto, PID: pid, Process: name, Local: l, Remote: r, State: defaultState(proto, f.state)}, true
}

// lsofAddr normalises `host:port`, `[v6]:port` or `*:port` into ip:port.
func lsofAddr(s string, ipv6 bool) (string, bool) {
	host, port, ok := splitHostPort(s)
	if !ok || port == "" || port == "*" {
		return "", false
	}
	return joinAddr(wildcardHost(host, ipv6), port), true
}

// splitHostPort splits on the last colon and strips IPv6 brackets.
func splitHostPort(s string) (host, port string, ok bool) {
	i := strings.LastIndex(s, ":")
	if i < 0 {
		return "", "", false
	}
	host, port = s[:i], s[i+1:]
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	return host, port, true
}
