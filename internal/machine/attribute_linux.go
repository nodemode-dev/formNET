//go:build linux

package machine

import (
	"bufio"
	"context"
	"encoding/binary"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const attributes = true

var nativeEndian = binary.NativeEndian

// lookupPort finds the socket whose local end is ip:port (netlink, or
// /proc/net/tcp and tcp6), while the open sockets and names of the newest
// few processes are read at the same time; a match there names the client
// even when it has exited since. Otherwise the pids just below the last one
// handed out are tried, then every process of the socket's user, newest
// first, until attributeBudget. Processes of other users cannot be read
// without privilege and are skipped.
func lookupPort(ctx context.Context, ip net.IP, port int) (int, string, bool) {
	newest := make(chan map[string]earlyOwner, 1)
	go func() { newest <- newestSockets("/proc") }()
	inode, uid, ok := socketOf("/proc", ip, port)
	early := <-newest
	if !ok {
		return 0, "", false
	}
	if hit, found := early["socket:["+inode+"]"]; found {
		return hit.pid, hit.name, hit.name != ""
	}
	pid := recentOwner("/proc", "socket:["+inode+"]", uid)
	if pid <= 0 {
		pid = inodeOwner(ctx, "/proc", inode, uid)
	}
	if pid <= 0 {
		return 0, "", false
	}
	pid = tgidOf("/proc", pid)
	name := readProcInfo("/proc", strconv.Itoa(pid)).name
	return pid, name, name != ""
}

// socketOf asks the kernel's socket diagnostics (netlink, what ss uses)
// for established TCP sockets first: it answers in well under a
// millisecond, where /proc/net/tcp lists every socket in every state and
// takes milliseconds on a busy machine, long enough for curl to exit.
// /proc/net/tcp and tcp6 are the fallback where netlink is refused.
func socketOf(root string, ip net.IP, port int) (inode string, uid int, ok bool) {
	if inode, uid, ok := diagSocket(port); ok {
		return inode, uid, true
	}
	want := strconv.Itoa(port)
	for _, table := range []string{"tcp", "tcp6"} {
		f, err := os.Open(filepath.Join(root, "net", table))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			fields := strings.Fields(sc.Text())
			if len(fields) < 10 {
				continue
			}
			host, p, good := procAddr(fields[1])
			if !good || p != want || fields[3] == "0A" {
				continue
			}
			if local := net.ParseIP(host); local == nil || !local.IsLoopback() {
				continue
			}
			u, err := strconv.Atoi(fields[7])
			if err != nil || fields[9] == "0" {
				continue
			}
			_ = f.Close()
			return fields[9], u, true
		}
		_ = f.Close()
	}
	return "", 0, false
}

const (
	netlinkSockDiag   = 4  // NETLINK_SOCK_DIAG
	sockDiagByFamily  = 20 // SOCK_DIAG_BY_FAMILY
	nlmsgHeaderLen    = 16
	inetDiagReqLen    = 56 // struct inet_diag_req_v2
	inetDiagMsgLen    = 72 // struct inet_diag_msg
	tcpEstablishedBit = 1 << 1
)

// diagSocket dumps established TCP sockets of both families and returns
// the one whose local port is port on a loopback address.
func diagSocket(port int) (string, int, bool) {
	fd, err := syscall.Socket(syscall.AF_NETLINK, syscall.SOCK_DGRAM|syscall.SOCK_CLOEXEC, netlinkSockDiag)
	if err != nil {
		return "", 0, false
	}
	defer syscall.Close(fd)
	tv := syscall.Timeval{Usec: 500000}
	_ = syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv)
	for seq, family := range []byte{syscall.AF_INET, syscall.AF_INET6} {
		req := make([]byte, nlmsgHeaderLen+inetDiagReqLen)
		nativeEndian.PutUint32(req[0:], uint32(len(req)))
		nativeEndian.PutUint16(req[4:], sockDiagByFamily)
		nativeEndian.PutUint16(req[6:], syscall.NLM_F_REQUEST|syscall.NLM_F_DUMP)
		nativeEndian.PutUint32(req[8:], uint32(seq+1))
		req[16], req[17] = family, syscall.IPPROTO_TCP
		nativeEndian.PutUint32(req[20:], tcpEstablishedBit)
		if err := syscall.Sendto(fd, req, 0, &syscall.SockaddrNetlink{Family: syscall.AF_NETLINK}); err != nil {
			return "", 0, false
		}
		if inode, uid, found, ok := diagRead(fd, family, port); !ok {
			return "", 0, false
		} else if found {
			return inode, uid, true
		}
	}
	return "", 0, false
}

// diagRead reads one dump to its end; ok is false when the dump failed.
func diagRead(fd int, family byte, port int) (inode string, uid int, found, ok bool) {
	buf := make([]byte, 64<<10)
	for {
		n, _, err := syscall.Recvfrom(fd, buf, 0)
		if err != nil {
			return "", 0, false, false
		}
		for b := buf[:n]; len(b) >= nlmsgHeaderLen; {
			size := int(nativeEndian.Uint32(b[0:]))
			kind := nativeEndian.Uint16(b[4:])
			if size < nlmsgHeaderLen || size > len(b) {
				return "", 0, false, false
			}
			switch kind {
			case syscall.NLMSG_DONE:
				return inode, uid, found, true
			case syscall.NLMSG_ERROR:
				return "", 0, false, false
			}
			msg := b[nlmsgHeaderLen:size]
			if !found && len(msg) >= inetDiagMsgLen && int(msg[4])<<8|int(msg[5]) == port {
				src := net.IP(append([]byte(nil), msg[8:24]...))
				if family == syscall.AF_INET {
					src = net.IP(append([]byte(nil), msg[8:12]...))
				}
				if src.IsLoopback() {
					if in := nativeEndian.Uint32(msg[68:]); in != 0 {
						inode, uid, found = strconv.FormatUint(uint64(in), 10), int(nativeEndian.Uint32(msg[64:])), true
					}
				}
			}
			b = b[min((size+3)&^3, len(b)):]
		}
	}
}

func inodeOwner(ctx context.Context, root, inode string, uid int) int {
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0
	}
	pids := make([]int, 0, len(entries))
	self := os.Getpid()
	for _, e := range entries {
		if pid, err := strconv.Atoi(e.Name()); err == nil && e.IsDir() && pid != self {
			pids = append(pids, pid)
		}
	}
	// Newest first: the pids at or below the last one handed out, highest
	// first, then the ones above it, which are older when pids have wrapped.
	last := lastPID(root)
	sort.Slice(pids, func(i, j int) bool {
		a, b := pids[i] <= last, pids[j] <= last
		if a != b {
			return a
		}
		return pids[i] > pids[j]
	})
	target := "socket:[" + inode + "]"
	deadline := time.Now().Add(attributeBudget)
	for _, pid := range pids {
		if ctx.Err() != nil || time.Now().After(deadline) {
			return 0
		}
		if holds(root, pid, target, uid) {
			return pid
		}
	}
	return 0
}

// recentPIDs is how many pids below the last one handed out are tried
// before /proc is listed: a client that just started is almost always there.
const recentPIDs = 64

// lastPID is the pid the kernel handed out last in this pid namespace, or
// a number above every pid when the system does not say.
func lastPID(root string) int {
	raw, err := os.ReadFile(filepath.Join(root, "sys", "kernel", "ns_last_pid"))
	if n, perr := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil && perr == nil && n > 0 {
		return n
	}
	return int(^uint(0) >> 1)
}

// recentOwner tries the pids just below the last one handed out, wrapping
// at pid_max, without listing /proc: well under a millisecond, before a
// short-lived client such as curl has exited.
func recentOwner(root, target string, uid int) int {
	last := lastPID(root)
	if last == int(^uint(0)>>1) {
		return 0
	}
	pidMax := 1 << 22
	if raw, err := os.ReadFile(filepath.Join(root, "sys", "kernel", "pid_max")); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil && n > 1 {
			pidMax = n
		}
	}
	self := os.Getpid()
	for i := 0; i < recentPIDs; i++ {
		pid := last - i
		if pid < 1 {
			pid += pidMax - 1
		}
		if pid != self && holds(root, pid, target, uid) {
			return pid
		}
	}
	return 0
}

// earlyPIDs is how many of the newest pids newestSockets reads.
const earlyPIDs = 8

type earlyOwner struct {
	pid  int
	name string
}

// newestSockets reads, before the socket is even known, the socket files
// and the names of the few processes started last: by the time netlink has
// named the socket a client such as curl may have exited, and what was read
// here still names it. A pid that is a thread is reported as its process.
func newestSockets(root string) map[string]earlyOwner {
	out := map[string]earlyOwner{}
	last := lastPID(root)
	if last == int(^uint(0)>>1) {
		return out
	}
	self := os.Getpid()
	for pid := last; pid > 0 && pid > last-earlyPIDs; pid-- {
		if pid == self {
			continue
		}
		dir := filepath.Join(root, strconv.Itoa(pid))
		fds, err := os.ReadDir(filepath.Join(dir, "fd"))
		if err != nil {
			continue
		}
		var socks []string
		for _, fd := range fds {
			if link, err := os.Readlink(filepath.Join(dir, "fd", fd.Name())); err == nil && strings.HasPrefix(link, "socket:[") {
				socks = append(socks, link)
			}
		}
		if len(socks) == 0 {
			continue
		}
		owner := earlyOwner{pid: tgidOf(root, pid), name: readProcInfo(root, strconv.Itoa(pid)).name}
		for _, s := range socks {
			if _, dup := out[s]; !dup {
				out[s] = owner
			}
		}
	}
	return out
}

// tgidOf returns the process a pid belongs to: itself, or for a thread its thread group.
func tgidOf(root string, pid int) int {
	raw, err := os.ReadFile(filepath.Join(root, strconv.Itoa(pid), "status"))
	if err != nil {
		return pid
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if v, ok := strings.CutPrefix(line, "Tgid:"); ok {
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
				return n
			}
		}
	}
	return pid
}

// holds reports whether process pid of user uid has target among its open files.
func holds(root string, pid int, target string, uid int) bool {
	dir := filepath.Join(root, strconv.Itoa(pid))
	fi, err := os.Stat(dir)
	if err != nil {
		return false
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && int(st.Uid) != uid {
		return false
	}
	fds, err := os.ReadDir(filepath.Join(dir, "fd"))
	if err != nil {
		return false
	}
	for _, fd := range fds {
		if link, err := os.Readlink(filepath.Join(dir, "fd", fd.Name())); err == nil && link == target {
			return true
		}
	}
	return false
}
