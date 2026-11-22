package machine

import (
	"encoding/csv"
	"strconv"
	"strings"
)

// parseNetstat reads Windows `netstat -ano -p tcp` and `-p udp` output by
// column position: proto, local, foreign, then the state (TCP only, in the
// system's language) and the pid last.
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
		pid, err := strconv.Atoi(fields[len(fields)-1])
		if err != nil {
			continue
		}
		state := ""
		if kind == "TCP" {
			state = netstatTCPState(strings.Join(fields[3:len(fields)-1], " "), fields[2])
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

var netstatEnglishStates = map[string]string{
	"LISTENING": "LISTEN", "ESTABLISHED": "ESTABLISHED", "TIME_WAIT": "TIME_WAIT", "CLOSE_WAIT": "CLOSE_WAIT",
	"SYN_SENT": "SYN_SENT", "SYN_RECEIVED": "SYN_RECEIVED", "FIN_WAIT_1": "FIN_WAIT_1", "FIN_WAIT_2": "FIN_WAIT_2",
	"LAST_ACK": "LAST_ACK", "CLOSING": "CLOSING", "CLOSED": "CLOSED", "BOUND": "BOUND", "DELETE_TCB": "DELETE_TCB",
}

func netstatTCPState(word, foreign string) string {
	if s, ok := netstatEnglishStates[strings.ToUpper(word)]; ok {
		return s
	}
	switch foreign {
	case "0.0.0.0:0", "[::]:0", "*:*":
		return "LISTEN"
	}
	return "ESTABLISHED"
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
