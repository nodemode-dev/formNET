package machine

import "testing"

const psSample = `    1 root             /sbin/launchd
 1228 [user]           /Applications/Google Chrome.app/Contents/Frameworks/Google Chrome Framework.framework/Versions/156.0.8078.49/Helpers/Google Chrome Helper (Aperitif).app/Contents/MacOS/Google Chrome Helper (Aperitif)
20191 [user]           /Users/[user]/.local/bin/claude
  365 root             syslogd
garbage line
`

func TestParsePSAndWithPS(t *testing.T) {
	ps := parsePS(psSample)
	if len(ps) != 4 {
		t.Fatalf("rows: %d %+v", len(ps), ps)
	}
	if ps[1228].name != "Google Chrome Helper (Aperitif)" || !hasSuffix(ps[1228].path, "/MacOS/Google Chrome Helper (Aperitif)") || ps[1228].user != "[user]" {
		t.Fatalf("spaces in comm: %+v", ps[1228])
	}
	if ps[20191].name != "claude" || ps[20191].path != "/Users/[user]/.local/bin/claude" {
		t.Fatalf("claude row: %+v", ps[20191])
	}
	if ps[365].name != "syslogd" || ps[365].path != "syslogd" {
		t.Fatalf("bare comm: %+v", ps[365])
	}
	info := map[int]procInfo{20191: {name: "2.1.314", user: "[user]"}, 777: {name: "onlylsof"}}
	merged := withPS(info, ps)
	if merged[20191].name != "claude" || merged[20191].path != "/Users/[user]/.local/bin/claude" {
		t.Fatalf("renamed: %+v", merged[20191])
	}
	if merged[777].name != "onlylsof" || merged[777].path != "" {
		t.Fatalf("process without a ps row must keep lsof's name: %+v", merged[777])
	}
	if info[20191].name != "2.1.314" {
		t.Fatal("withPS must not mutate its input")
	}
	conns := []Connection{{PID: 20191, Process: "2.1.314"}, {PID: 1, Process: "unknown"}}
	fixed := renamed(conns, merged)
	if fixed[0].Process != "claude" || fixed[1].Process != "unknown" || conns[0].Process != "2.1.314" {
		t.Fatalf("renamed: %+v (input %+v)", fixed, conns)
	}
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
