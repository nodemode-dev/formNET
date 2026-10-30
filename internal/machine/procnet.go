package machine

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var tcpStates = map[string]string{
	"01": "ESTABLISHED", "02": "SYN_SENT", "03": "SYN_RECV", "04": "FIN_WAIT1",
	"05": "FIN_WAIT2", "06": "TIME_WAIT", "07": "CLOSE", "08": "CLOSE_WAIT",
	"09": "LAST_ACK", "0A": "LISTEN", "0B": "CLOSING",
}

// procSocket is one row of /proc/net/{tcp,tcp6,udp,udp6} before its owner is known.
type procSocket struct {
	proto  string
	local  string
	remote string
	state  string
	inode  string
}

// readProc reads the socket tables and owners under root (normally /proc).
func readProc(root string) ([]Connection, map[int]procInfo, error) {
	var sockets []procSocket
	found := 0
	for _, table := range []string{"tcp", "tcp6", "udp", "udp6"} {
		raw, err := os.ReadFile(filepath.Join(root, "net", table))
		if err != nil {
			continue
		}
		found++
		sockets = append(sockets, parseProcNet(table, string(raw))...)
	}
	if found == 0 {
		return nil, nil, fmt.Errorf("no socket tables under %s", filepath.Join(root, "net"))
	}
	owners, info := readProcOwners(root)
	conns := make([]Connection, 0, len(sockets))
	for _, s := range sockets {
		pid := owners[s.inode]
		conns = append(conns, Connection{Proto: s.proto, PID: pid, Process: info[pid].name, Local: s.local, Remote: s.remote, State: s.state})
	}
	return conns, info, nil
}

// parseProcNet parses one table. Columns: sl local rem st tx:rx tr:when retrnsmt uid timeout inode.
func parseProcNet(table, text string) []procSocket {
	var out []procSocket
	sc := bufio.NewScanner(strings.NewReader(text))
	first := true
	for sc.Scan() {
		if first {
			first = false
			continue
		}
		fields := strings.Fields(sc.Text())
		if len(fields) < 10 {
			continue
		}
		local, lport, ok1 := procAddr(fields[1])
		remote, rport, ok2 := procAddr(fields[2])
		if !ok1 || !ok2 {
			continue
		}
		state := tcpStates[strings.ToUpper(fields[3])]
		udp := strings.HasPrefix(table, "udp")
		if udp && state != "ESTABLISHED" {
			state = "UDP"
		}
		s := procSocket{proto: table, local: joinAddr(local, lport), state: state, inode: fields[9]}
		if rport != "0" && state != "LISTEN" {
			s.remote = joinAddr(remote, rport)
		}
		out = append(out, s)
	}
	return out
}

// procAddr decodes HEXADDR:HEXPORT. IPv4 is one little-endian word; IPv6 is
// four little-endian words in order.
func procAddr(s string) (ip, port string, ok bool) {
	h, p, found := strings.Cut(s, ":")
	if !found {
		return "", "", false
	}
	raw, err := hex.DecodeString(h)
	if err != nil {
		return "", "", false
	}
	n, err := strconv.ParseUint(p, 16, 16)
	if err != nil {
		return "", "", false
	}
	switch len(raw) {
	case 4:
		ip = net.IPv4(raw[3], raw[2], raw[1], raw[0]).String()
	case 16:
		b := make([]byte, 16)
		for w := 0; w < 4; w++ {
			for i := 0; i < 4; i++ {
				b[w*4+i] = raw[w*4+3-i]
			}
		}
		ip = net.IP(b).String()
	default:
		return "", "", false
	}
	return ip, strconv.FormatUint(n, 10), true
}

// readProcOwners maps socket inodes to pids and gathers names and paths.
// Processes the user may not read are skipped.
func readProcOwners(root string) (map[string]int, map[int]procInfo) {
	owners := map[string]int{}
	info := map[int]procInfo{}
	entries, err := os.ReadDir(root)
	if err != nil {
		return owners, info
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || !e.IsDir() {
			continue
		}
		fds, err := os.ReadDir(filepath.Join(root, e.Name(), "fd"))
		if err != nil {
			continue
		}
		has := false
		for _, fd := range fds {
			target, err := os.Readlink(filepath.Join(root, e.Name(), "fd", fd.Name()))
			if err != nil {
				continue
			}
			if inode, ok := strings.CutPrefix(target, "socket:["); ok {
				owners[strings.TrimSuffix(inode, "]")] = pid
				has = true
			}
		}
		if has {
			info[pid] = readProcInfo(root, e.Name())
		}
	}
	return owners, info
}

// commLimit is the kernel's comm length; a name that long is probably cut short.
const commLimit = 15

func readProcInfo(root, pid string) procInfo {
	pi := procInfo{}
	if comm, err := os.ReadFile(filepath.Join(root, pid, "comm")); err == nil {
		pi.name = strings.TrimSpace(string(comm))
	}
	if exe, err := os.Readlink(filepath.Join(root, pid, "exe")); err == nil {
		pi.path = exe
		if len(pi.name) >= commLimit && filepath.Base(exe) != "" {
			pi.name = filepath.Base(exe)
		}
	}
	return pi
}
