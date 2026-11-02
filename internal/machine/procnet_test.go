package machine

import (
	"os"
	"path/filepath"
	"testing"
)

const procTCP = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 11111 1 0000000000000000 100 0 0 10 0
   1: 7F00A8C0:C350 5EBC7A4A:01BB 01 00000000:00000000 00:00000000 00000000  1000        0 22222 1 0000000000000000 20 4 30 10 -1
   2: 7F00A8C0:C351 5EBC7A4A:01BB 06 00000000:00000000 00:00000000 00000000     0        0 0 3 0000000000000000
`

const procTCP6 = `  sl  local_address                         remote_address                        st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000000000000000000000000000:22B8 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 33333 1 0000000000000000 100 0 0 10 0
   1: 0000000000000000FFFF00000100007F:A2E6 0000000000000000FFFF00000100007F:2238 01 00000000:00000000 00:00000000 00000000  1000        0 44444 1 0000000000000000 20 4 1 10 -1
`

const procUDP = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode ref pointer drops
  40: 00000000:14E9 00000000:0000 07 00000000:00000000 00:00000000 00000000  1000        0 55555 2 0000000000000000 0
  41: 7F00A8C0:D6F3 5EBC7A4A:01BB 01 00000000:00000000 00:00000000 00000000  1000        0 66666 2 0000000000000000 0
`

func TestProcAddr(t *testing.T) {
	ip, port, ok := procAddr("0100007F:1F90")
	if !ok || ip != "127.0.0.1" || port != "8080" {
		t.Fatalf("ipv4: %s %s %v", ip, port, ok)
	}
	ip, port, ok = procAddr("0000000000000000FFFF00000100007F:A2E6")
	if !ok || ip != "127.0.0.1" || port != "41702" { // Go prints a v4-mapped address in v4 form
		t.Fatalf("mapped ipv6: %s %s %v", ip, port, ok)
	}
	ip, _, ok = procAddr("B80D01200000000000000000C0000000:0050")
	if !ok || ip != "2001:db8::c0" {
		t.Fatalf("ipv6 words: %s %v", ip, ok)
	}
	if _, _, ok := procAddr("nonsense"); ok {
		t.Fatal("garbage accepted")
	}
}

func TestParseProcNet(t *testing.T) {
	tcp := parseProcNet("tcp", procTCP)
	if len(tcp) != 3 {
		t.Fatalf("tcp rows: %d", len(tcp))
	}
	if tcp[0].local != "127.0.0.1:8080" || tcp[0].state != "LISTEN" || tcp[0].remote != "" || tcp[0].inode != "11111" {
		t.Fatalf("listen row: %+v", tcp[0])
	}
	if tcp[1].local != "192.168.0.127:50000" || tcp[1].remote != "74.122.188.94:443" || tcp[1].state != "ESTABLISHED" {
		t.Fatalf("established row: %+v", tcp[1])
	}
	if tcp[2].state != "TIME_WAIT" || tcp[2].inode != "0" {
		t.Fatalf("time_wait row: %+v", tcp[2])
	}
	tcp6 := parseProcNet("tcp6", procTCP6)
	if tcp6[0].local != "[::]:8888" || tcp6[0].proto != "tcp6" || tcp6[1].remote != "127.0.0.1:8760" {
		t.Fatalf("tcp6 rows: %+v", tcp6)
	}
	udp := parseProcNet("udp", procUDP)
	if udp[0].state != "UDP" || udp[0].remote != "" || udp[0].local != "0.0.0.0:5353" {
		t.Fatalf("unconnected udp: %+v", udp[0])
	}
	if udp[1].state != "ESTABLISHED" || udp[1].remote != "74.122.188.94:443" {
		t.Fatalf("connected udp: %+v", udp[1])
	}
}

func TestReadProcJoinsOwners(t *testing.T) {
	root := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(root, "net"), 0o755))
	must(os.WriteFile(filepath.Join(root, "net", "tcp"), []byte(procTCP), 0o644))
	must(os.WriteFile(filepath.Join(root, "net", "udp"), []byte(procUDP), 0o644))
	// pid 4242 owns inodes 11111 and 66666; pid 99 has no sockets; "self" is not a pid.
	must(os.MkdirAll(filepath.Join(root, "4242", "fd"), 0o755))
	must(os.Symlink("socket:[11111]", filepath.Join(root, "4242", "fd", "3")))
	must(os.Symlink("socket:[66666]", filepath.Join(root, "4242", "fd", "4")))
	must(os.Symlink("/dev/null", filepath.Join(root, "4242", "fd", "0")))
	must(os.WriteFile(filepath.Join(root, "4242", "comm"), []byte("formnet\n"), 0o644))
	must(os.Symlink("/usr/local/bin/formnet", filepath.Join(root, "4242", "exe")))
	must(os.MkdirAll(filepath.Join(root, "99", "fd"), 0o755))
	must(os.WriteFile(filepath.Join(root, "99", "comm"), []byte("idle\n"), 0o644))
	must(os.MkdirAll(filepath.Join(root, "self", "fd"), 0o755))
	// pid 5151 has a comm cut at 15 characters; its exe gives the full name.
	must(os.MkdirAll(filepath.Join(root, "5151", "fd"), 0o755))
	must(os.Symlink("socket:[22222]", filepath.Join(root, "5151", "fd", "7")))
	must(os.WriteFile(filepath.Join(root, "5151", "comm"), []byte("averyveryverylo\n"), 0o644))
	must(os.Symlink("/opt/averyveryverylongprogramname", filepath.Join(root, "5151", "exe")))

	conns, info, err := readProc(root)
	must(err)
	if len(conns) != 5 {
		t.Fatalf("connections: %d", len(conns))
	}
	if info[4242].name != "formnet" || info[4242].path != filepath.FromSlash("/usr/local/bin/formnet") {
		t.Fatalf("info: %+v", info)
	}
	if info[5151].name != "averyveryverylongprogramname" {
		t.Fatalf("truncated comm must take the exe name: %+v", info[5151])
	}
	if _, present := info[99]; present {
		t.Fatal("a process without sockets must not be listed")
	}
	owned := 0
	for _, c := range conns {
		if c.PID == 4242 {
			owned++
			if c.Process != "formnet" {
				t.Fatalf("owner name: %+v", c)
			}
		}
	}
	if owned != 2 {
		t.Fatalf("owned connections: %d", owned)
	}
	snap := assemble("proc", conns, info)
	if len(snap.Processes) != 2 || snap.Processes[0].PID != 4242 || snap.Processes[0].Connections != 2 {
		t.Fatalf("processes: %+v", snap.Processes)
	}
	if _, _, err := readProc(filepath.Join(root, "missing")); err == nil {
		t.Fatal("a root without tables must fail")
	}
}
