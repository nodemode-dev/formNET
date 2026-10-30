package machine

import (
	"encoding/csv"
	"strconv"
	"strings"
)

// parseNetstat reads Windows `netstat -ano -p tcp` and `-p udp` output.
// TCP rows have proto, local, foreign, state, pid; UDP rows have no state.
func parseNetstat(text string) []Connection {
	var out []Connection
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		kind := strings.ToUpper(fields[0])
		if kind != "TCP" && kind != "UDP" {
			continue
		}
		var state, pidText string
		if kind == "TCP" && len(fields) >= 5 {
			state, pidText = fields[3], fields[4]
		} else {
			pidText = fields[3]
		}
		pid, err := strconv.Atoi(pidText)
		if err != nil {
			continue
		}
		ipv6 := strings.HasPrefix(fields[1], "[")
		proto := protoFor(kind, ipv6)
		local, ok := netstatAddr(fields[1], ipv6)
		if !ok {
			continue
		}
		state = defaultState(proto, state)
		remote := ""
		if state != "LISTEN" {
			if r, ok := netstatAddr(fields[2], ipv6); ok && !strings.HasSuffix(r, ":0") {
				remote = r
			}
		}
		out = append(out, Connection{Proto: proto, PID: pid, Local: local, Remote: remote, State: state})
	}
	return out
}

func netstatAddr(s string, ipv6 bool) (string, bool) {
	host, port, ok := splitHostPort(s)
	if !ok || port == "" || port == "*" {
		return "", false
	}
	return joinAddr(wildcardHost(host, ipv6), port), true
}

// parseTasklist reads `tasklist /fo csv /nh`: "name","pid","session","#","mem".
func parseTasklist(text string) map[int]string {
	out := map[int]string{}
	r := csv.NewReader(strings.NewReader(text))
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		return out
	}
	for _, row := range rows {
		if len(row) < 2 {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(row[1]))
		if err != nil {
			continue
		}
		out[pid] = strings.TrimSpace(row[0])
	}
	return out
}
