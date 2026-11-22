//go:build windows

package terminal

import (
	"errors"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

const drain = 250 * time.Millisecond

var (
	kernel32       = windows.NewLazySystemDLL("kernel32.dll")
	procConPTY     = kernel32.NewProc("CreatePseudoConsole")
	procUpdateAttr = kernel32.NewProc("UpdateProcThreadAttribute")
	errNoConPTY    = errors.New("the terminal needs Windows 10 version 1809 or later (ConPTY)")
)

type winProc struct {
	mu      sync.Mutex
	hpc     windows.Handle
	in      *os.File
	out     *os.File
	process windows.Handle
	job     windows.Handle
	exited  chan struct{}
	status  int
}

func supported() (bool, string) {
	if procConPTY.Find() != nil || procUpdateAttr.Find() != nil {
		return false, errNoConPTY.Error()
	}
	return true, ""
}

func start(argv []string, dir string, env []string, cols, rows uint16) (proc, error) {
	if ok, _ := supported(); !ok {
		return nil, errNoConPTY
	}
	var inR, inW, outR, outW windows.Handle
	if err := windows.CreatePipe(&inR, &inW, nil, 0); err != nil {
		return nil, err
	}
	if err := windows.CreatePipe(&outR, &outW, nil, 0); err != nil {
		_ = windows.CloseHandle(inR)
		_ = windows.CloseHandle(inW)
		return nil, err
	}
	var hpc windows.Handle
	err := windows.CreatePseudoConsole(windows.Coord{X: int16(cols), Y: int16(rows)}, inR, outW, 0, &hpc)
	_ = windows.CloseHandle(inR)
	_ = windows.CloseHandle(outW)
	if err != nil {
		_ = windows.CloseHandle(inW)
		_ = windows.CloseHandle(outR)
		return nil, err
	}
	p := &winProc{hpc: hpc, in: os.NewFile(uintptr(inW), "conpty-in"), out: os.NewFile(uintptr(outR), "conpty-out"), exited: make(chan struct{}), status: -1}
	if err := p.spawn(argv, dir, env); err != nil {
		p.release()
		return nil, err
	}
	go p.wait()
	return p, nil
}

func (p *winProc) spawn(argv []string, dir string, env []string) error {
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return err
	}
	defer attrs.Delete()
	if r, _, e := procUpdateAttr.Call(uintptr(unsafe.Pointer(attrs.List())), 0, windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, uintptr(p.hpc), unsafe.Sizeof(p.hpc), 0, 0); r == 0 {
		return e
	}
	si := windows.StartupInfoEx{ProcThreadAttributeList: attrs.List()}
	si.Cb = uint32(unsafe.Sizeof(si))
	si.Flags = windows.STARTF_USESTDHANDLES
	line, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(argv))
	if err != nil {
		return err
	}
	folder, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return err
	}
	block := envBlock(env)
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return err
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		_ = windows.CloseHandle(job)
		return err
	}
	var pi windows.ProcessInformation
	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_UNICODE_ENVIRONMENT | windows.CREATE_SUSPENDED)
	if err := windows.CreateProcess(nil, line, nil, nil, false, flags, &block[0], folder, &si.StartupInfo, &pi); err != nil {
		_ = windows.CloseHandle(job)
		return err
	}
	if err := windows.AssignProcessToJobObject(job, pi.Process); err != nil {
		_ = windows.TerminateProcess(pi.Process, 1)
		_ = windows.CloseHandle(pi.Thread)
		_ = windows.CloseHandle(pi.Process)
		_ = windows.CloseHandle(job)
		return err
	}
	_, _ = windows.ResumeThread(pi.Thread)
	_ = windows.CloseHandle(pi.Thread)
	p.process, p.job = pi.Process, job
	return nil
}

func (p *winProc) wait() {
	_, _ = windows.WaitForSingleObject(p.process, windows.INFINITE)
	var c uint32
	if windows.GetExitCodeProcess(p.process, &c) == nil {
		p.status = int(c)
	}
	_ = windows.CloseHandle(p.process)
	close(p.exited)
}

func envBlock(env []string) []uint16 {
	index := map[string]int{}
	keep := []string{}
	for _, kv := range env {
		if strings.IndexByte(kv, 0) >= 0 {
			continue
		}
		key, from := kv, 0
		if strings.HasPrefix(kv, "=") {
			from = 1
		}
		if i := strings.IndexByte(kv[from:], '='); i >= 0 {
			key = kv[:from+i]
		}
		key = strings.ToUpper(key)
		if at, ok := index[key]; ok {
			keep[at] = kv
			continue
		}
		index[key] = len(keep)
		keep = append(keep, kv)
	}
	if _, ok := index["SYSTEMROOT"]; !ok {
		if v := os.Getenv("SYSTEMROOT"); v != "" {
			keep = append(keep, "SYSTEMROOT="+v)
		}
	}
	block := []uint16{}
	for _, kv := range keep {
		block = append(block, utf16.Encode([]rune(kv))...)
		block = append(block, 0)
	}
	if len(block) == 0 {
		block = append(block, 0)
	}
	return append(block, 0)
}

func (p *winProc) Read(b []byte) (int, error)  { return p.out.Read(b) }
func (p *winProc) Write(b []byte) (int, error) { return p.in.Write(b) }
func (p *winProc) done() <-chan struct{}       { return p.exited }
func (p *winProc) code() int                   { return p.status }

func (p *winProc) resize(cols, rows uint16) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.hpc == 0 {
		return nil
	}
	return windows.ResizePseudoConsole(p.hpc, windows.Coord{X: int16(cols), Y: int16(rows)})
}

func (p *winProc) closeConsole() {
	p.mu.Lock()
	hpc := p.hpc
	p.hpc = 0
	p.mu.Unlock()
	if hpc != 0 {
		windows.ClosePseudoConsole(hpc)
	}
}

func (p *winProc) end(grace time.Duration) {
	if p.process == 0 {
		return
	}
	select {
	case <-p.exited:
	default:
		if grace > 0 {
			p.closeConsole()
			select {
			case <-p.exited:
			case <-time.After(grace):
			}
		}
	}
	_ = windows.TerminateJobObject(p.job, 1)
	select {
	case <-p.exited:
	case <-time.After(2 * time.Second):
	}
}

func (p *winProc) release() {
	p.closeConsole()
	_ = p.in.Close()
	_ = p.out.Close()
	p.mu.Lock()
	job := p.job
	p.job = 0
	p.mu.Unlock()
	if job != 0 {
		_ = windows.CloseHandle(job)
	}
}
