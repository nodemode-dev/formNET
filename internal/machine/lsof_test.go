package machine

import "testing"

const lsofSample = `p617
crapportd
Lgregorybarnes
f10
tIPv4
PTCP
n*:49154
TST=LISTEN
TQR=0
TQS=0
f25
tIPv4
PTCP
n192.168.0.127:49154->192.168.0.227:54129
TST=ESTABLISHED
TQR=0
TQS=0
f26
tIPv4
PUDP
n*:3722
p622
cidentityservicesd
Lgregorybarnes
f5
tIPv4
PUDP
n*:*
f52
tIPv6
PTCP
n[fe80:19::403e:5b91:6ff0:affe]:1024->[fe80:19::200f:2717:bedd:f695]:1024
TST=ESTABLISHED
TQR=0
TQS=0
f60
tIPv4
PUDP
n192.168.0.127:55131->74.125.138.94:443
p9000
cGoogle Chrome Helper
Lgregorybarnes
f30
tIPv6
PTCP
n*:8765
TST=LISTEN
`

func TestParseLsof(t *testing.T) {
	conns, info := parseLsof(lsofSample)
	if len(conns) != 6 {
		t.Fatalf("connections: %d, want 6 (unbound *:* left out)\n%+v", len(conns), conns)
	}
	if info[617].name != "rapportd" || info[617].user != "gregorybarnes" || info[9000].name != "Google Chrome Helper" {
		t.Fatalf("process info: %+v", info)
	}
	want := []Connection{
		{Proto: "tcp", PID: 617, Process: "rapportd", Local: "0.0.0.0:49154", Remote: "", State: "LISTEN"},
		{Proto: "tcp", PID: 617, Process: "rapportd", Local: "192.168.0.127:49154", Remote: "192.168.0.227:54129", State: "ESTABLISHED"},
		{Proto: "udp", PID: 617, Process: "rapportd", Local: "0.0.0.0:3722", Remote: "", State: "UDP"},
		{Proto: "tcp6", PID: 622, Process: "identityservicesd", Local: "[fe80:19::403e:5b91:6ff0:affe]:1024", Remote: "[fe80:19::200f:2717:bedd:f695]:1024", State: "ESTABLISHED"},
		{Proto: "udp", PID: 622, Process: "identityservicesd", Local: "192.168.0.127:55131", Remote: "74.125.138.94:443", State: "UDP"},
		{Proto: "tcp6", PID: 9000, Process: "Google Chrome Helper", Local: "[::]:8765", Remote: "", State: "LISTEN"},
	}
	for i, w := range want {
		g := conns[i]
		if g.Proto != w.Proto || g.PID != w.PID || g.Process != w.Process || g.Local != w.Local || g.Remote != w.Remote || g.State != w.State {
			t.Errorf("conn %d:\n got %+v\nwant %+v", i, g, w)
		}
	}
}

func TestAssembleSortsAndCounts(t *testing.T) {
	conns, info := parseLsof(lsofSample)
	info[617] = procInfo{name: info[617].name, user: info[617].user, path: "/usr/libexec/rapportd", bytesIn: 10, bytesOut: 20}
	snap := assemble("lsof+nettop", conns, info)
	if !snap.Supported || snap.Taken.IsZero() || snap.Source != "lsof+nettop" {
		t.Fatalf("snapshot header: %+v", snap)
	}
	if len(snap.Processes) != 3 || snap.Processes[0].PID != 617 || snap.Processes[0].Connections != 3 || snap.Processes[0].BytesOut != 20 {
		t.Fatalf("processes: %+v", snap.Processes)
	}
	if snap.Processes[1].Name != "identityservicesd" || snap.Processes[2].Name != "Google Chrome Helper" {
		t.Fatalf("process order: %+v", snap.Processes)
	}
	if snap.Connections[0].Process != "Google Chrome Helper" || snap.Connections[len(snap.Connections)-1].Process != "rapportd" {
		t.Fatalf("connection order: %+v", snap.Connections)
	}
	for _, c := range snap.Connections {
		if c.FirstSeen.IsZero() || c.LastSeen.IsZero() {
			t.Fatalf("seen times unset: %+v", c)
		}
		if c.PID == 617 && c.Path != "/usr/libexec/rapportd" {
			t.Fatalf("connection must carry the executable path: %+v", c)
		}
		if c.PID == 622 && c.Path != "" {
			t.Fatalf("unknown path must stay empty: %+v", c)
		}
	}
}
