//go:build unix

package terminal

import (
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/creack/pty"
)

const drain = time.Second

type unixProc struct {
	f      *os.File
	cmd    *exec.Cmd
	exited chan struct{}
	status int
}

func supported() (bool, string) { return true, "" }

func start(argv []string, dir string, env []string, cols, rows uint16) (proc, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir, cmd.Env = dir, env
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: cols, Rows: rows})
	if err != nil {
		return nil, err
	}
	p := &unixProc{f: f, cmd: cmd, exited: make(chan struct{}), status: -1}
	go func() {
		_ = cmd.Wait()
		if cmd.ProcessState != nil {
			p.status = cmd.ProcessState.ExitCode()
		}
		close(p.exited)
	}()
	return p, nil
}

func (p *unixProc) Read(b []byte) (int, error)  { return p.f.Read(b) }
func (p *unixProc) Write(b []byte) (int, error) { return p.f.Write(b) }
func (p *unixProc) done() <-chan struct{}       { return p.exited }
func (p *unixProc) code() int                   { return p.status }

func (p *unixProc) resize(cols, rows uint16) error {
	return pty.Setsize(p.f, &pty.Winsize{Cols: cols, Rows: rows})
}

func (p *unixProc) gone() bool {
	select {
	case <-p.exited:
		return true
	default:
		return false
	}
}

func (p *unixProc) signal(sig syscall.Signal) {
	sid := p.cmd.Process.Pid
	self := os.Getpid()
	for _, pid := range members(sid) {
		if pid != self && pid > 1 {
			_ = syscall.Kill(pid, sig)
		}
	}
	if !p.gone() {
		_ = syscall.Kill(-sid, sig)
	}
}

func (p *unixProc) end(grace time.Duration) {
	if !p.gone() && grace > 0 {
		p.signal(syscall.SIGHUP)
		p.signal(syscall.SIGCONT)
		select {
		case <-p.exited:
		case <-time.After(grace):
		}
	}
	p.signal(syscall.SIGKILL)
	select {
	case <-p.exited:
	case <-time.After(2 * time.Second):
	}
}

func (p *unixProc) release() { _ = p.f.Close() }
