package machine

import "testing"

const netstatSample = `
Active Connections

  Proto  Local Address          Foreign Address        State           PID
  TCP    0.0.0.0:135            0.0.0.0:0              LISTENING       1096
  TCP    192.168.1.20:51234     142.250.80.46:443      ESTABLISHED     7788
  TCP    [::]:135               [::]:0                 LISTENING       1096
  TCP    [fe80::1%5]:49670      [fe80::2%5]:445        ESTABLISHED     4
  UDP    0.0.0.0:5353           *:*                                    3344
  UDP    [::1]:1900             *:*                                    3344
`

const tasklistSample = `"System Idle Process","0","Services","0","8 K"
"svchost.exe","1096","Services","0","12,344 K"
"chrome.exe","7788","Console","1","210,004 K"
"Weird, Name.exe","3344","Console","1","1 K"
`

func TestParseNetstat(t *testing.T) {
	conns := parseNetstat(netstatSample)
	if len(conns) != 6 {
		t.Fatalf("rows: %d %+v", len(conns), conns)
	}
	want := []Connection{
		{Proto: "tcp", PID: 1096, Local: "0.0.0.0:135", State: "LISTEN"},
		{Proto: "tcp", PID: 7788, Local: "192.168.1.20:51234", Remote: "142.250.80.46:443", State: "ESTABLISHED"},
		{Proto: "tcp6", PID: 1096, Local: "[::]:135", State: "LISTEN"},
		{Proto: "tcp6", PID: 4, Local: "[fe80::1%5]:49670", Remote: "[fe80::2%5]:445", State: "ESTABLISHED"},
		{Proto: "udp", PID: 3344, Local: "0.0.0.0:5353", State: "UDP"},
		{Proto: "udp6", PID: 3344, Local: "[::1]:1900", State: "UDP"},
	}
	for i, w := range want {
		g := conns[i]
		if g.Proto != w.Proto || g.PID != w.PID || g.Local != w.Local || g.Remote != w.Remote || g.State != w.State {
			t.Errorf("row %d:\n got %+v\nwant %+v", i, g, w)
		}
	}
}

func TestParseTasklist(t *testing.T) {
	names := parseTasklist(tasklistSample)
	if names[1096] != "svchost.exe" || names[7788] != "chrome.exe" || names[3344] != "Weird, Name.exe" || names[0] != "System Idle Process" {
		t.Fatalf("%+v", names)
	}
	info := map[int]procInfo{}
	for pid, n := range names {
		info[pid] = procInfo{name: n}
	}
	snap := assemble("netstat", parseNetstat(netstatSample), info)
	if snap.Processes[0].Name != "svchost.exe" && snap.Processes[0].Name != "Weird, Name.exe" {
		t.Fatalf("named processes: %+v", snap.Processes)
	}
	for _, c := range snap.Connections {
		if c.PID != 4 && c.Process == "" {
			t.Fatalf("unnamed connection: %+v", c)
		}
	}
}
