//go:build windows

package sysproxy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

const settingsKey = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

const (
	internetOptionRefresh         = 37
	internetOptionSettingsChanged = 39
)

var (
	advapi32           = syscall.NewLazyDLL("advapi32.dll")
	procRegSetValueEx  = advapi32.NewProc("RegSetValueExW")
	procRegDeleteValue = advapi32.NewProc("RegDeleteValueW")
	wininet            = syscall.NewLazyDLL(systemDLL("wininet.dll"))
	procInternetSetOpt = wininet.NewProc("InternetSetOptionW")
)

// systemDLL names a DLL in the System32 folder, so it is never loaded from the program's own folder.
func systemDLL(name string) string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		return name
	}
	return filepath.Join(root, "System32", name)
}

func platformSupported() (bool, string) { return true, "" }

func openKey(access uint32) (syscall.Handle, error) {
	name, err := syscall.UTF16PtrFromString(settingsKey)
	if err != nil {
		return 0, err
	}
	var h syscall.Handle
	if err := syscall.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, name, 0, access, &h); err != nil {
		return 0, err
	}
	return h, nil
}

func queryString(h syscall.Handle, value string) (*string, error) {
	name, err := syscall.UTF16PtrFromString(value)
	if err != nil {
		return nil, err
	}
	var typ, size uint32
	err = syscall.RegQueryValueEx(h, name, nil, &typ, nil, &size)
	if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", value, err)
	}
	if typ != syscall.REG_SZ && typ != syscall.REG_EXPAND_SZ {
		return nil, fmt.Errorf("reading %s: it is not a text value", value)
	}
	buf := make([]uint16, size/2+2)
	size = uint32(len(buf) * 2)
	if err := syscall.RegQueryValueEx(h, name, nil, &typ, (*byte)(unsafe.Pointer(&buf[0])), &size); err != nil {
		return nil, fmt.Errorf("reading %s: %w", value, err)
	}
	s := syscall.UTF16ToString(buf)
	return &s, nil
}

func queryDWORD(h syscall.Handle, value string) (*uint32, error) {
	name, err := syscall.UTF16PtrFromString(value)
	if err != nil {
		return nil, err
	}
	var typ, v uint32
	size := uint32(4)
	err = syscall.RegQueryValueEx(h, name, nil, &typ, (*byte)(unsafe.Pointer(&v)), &size)
	if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", value, err)
	}
	if typ != syscall.REG_DWORD || size != 4 {
		return nil, fmt.Errorf("reading %s: it is not a number value", value)
	}
	return &v, nil
}

func setString(h syscall.Handle, value, data string) error {
	name, err := syscall.UTF16PtrFromString(value)
	if err != nil {
		return err
	}
	text, err := syscall.UTF16FromString(data)
	if err != nil {
		return err
	}
	rc, _, _ := procRegSetValueEx.Call(uintptr(h), uintptr(unsafe.Pointer(name)), 0, syscall.REG_SZ, uintptr(unsafe.Pointer(&text[0])), uintptr(len(text)*2))
	if rc != 0 {
		return fmt.Errorf("writing %s: %w", value, syscall.Errno(rc))
	}
	return nil
}

func setDWORD(h syscall.Handle, value string, v uint32) error {
	name, err := syscall.UTF16PtrFromString(value)
	if err != nil {
		return err
	}
	rc, _, _ := procRegSetValueEx.Call(uintptr(h), uintptr(unsafe.Pointer(name)), 0, syscall.REG_DWORD, uintptr(unsafe.Pointer(&v)), 4)
	if rc != 0 {
		return fmt.Errorf("writing %s: %w", value, syscall.Errno(rc))
	}
	return nil
}

func deleteValue(h syscall.Handle, value string) error {
	name, err := syscall.UTF16PtrFromString(value)
	if err != nil {
		return err
	}
	rc, _, _ := procRegDeleteValue.Call(uintptr(h), uintptr(unsafe.Pointer(name)))
	if rc != 0 && syscall.Errno(rc) != syscall.ERROR_FILE_NOT_FOUND {
		return fmt.Errorf("removing %s: %w", value, syscall.Errno(rc))
	}
	return nil
}

// notify tells running programs that use WinINET to read the settings again.
func notify() {
	_, _, _ = procInternetSetOpt.Call(0, internetOptionSettingsChanged, 0, 0)
	_, _, _ = procInternetSetOpt.Call(0, internetOptionRefresh, 0, 0)
}

func readSystem() (snapshot, error) {
	snap := snapshot{OS: "windows", WinINET: &wininetState{}}
	h, err := openKey(syscall.KEY_QUERY_VALUE)
	if errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) {
		return snap, nil
	}
	if err != nil {
		return snap, fmt.Errorf("opening the Internet Settings of this user: %w", err)
	}
	defer syscall.RegCloseKey(h)
	w := snap.WinINET
	if w.ProxyEnable, err = queryDWORD(h, "ProxyEnable"); err != nil {
		return snap, err
	}
	if w.ProxyServer, err = queryString(h, "ProxyServer"); err != nil {
		return snap, err
	}
	if w.ProxyOverride, err = queryString(h, "ProxyOverride"); err != nil {
		return snap, err
	}
	auto, err := queryString(h, "AutoConfigURL")
	if err != nil {
		return snap, err
	}
	if auto != nil {
		w.AutoConfigURL = strings.TrimSpace(*auto)
	}
	return snap, nil
}

// servers reads ProxyServer: one host:port for every protocol, or
// protocol=host:port pairs separated by semicolons.
func servers(value string) (web, secure string) {
	value = strings.TrimSpace(value)
	if !strings.Contains(value, "=") {
		return value, value
	}
	for _, part := range strings.Split(value, ";") {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		v = strings.TrimPrefix(strings.TrimSpace(v), "http://")
		switch strings.ToLower(strings.TrimSpace(k)) {
		case "http":
			web = v
		case "https":
			secure = v
		}
	}
	return web, secure
}

func enabled(w *wininetState) bool {
	return w != nil && w.ProxyEnable != nil && *w.ProxyEnable != 0 && w.ProxyServer != nil
}

func pointsAt(cur snapshot, addr string) bool {
	if !enabled(cur.WinINET) {
		return false
	}
	web, secure := servers(*cur.WinINET.ProxyServer)
	return web == addr || secure == addr
}

func describe(cur snapshot) []Service {
	s := Service{Name: "Internet Settings of this user"}
	if enabled(cur.WinINET) {
		s.Web, s.Secure = servers(*cur.WinINET.ProxyServer)
	}
	return []Service{s}
}

func autoConfig(cur snapshot) bool {
	return cur.WinINET != nil && cur.WinINET.AutoConfigURL != ""
}

// withLocal keeps the person's bypass list and makes sure it holds <local>.
func withLocal(override *string) string {
	if override == nil || strings.TrimSpace(*override) == "" {
		return "<local>"
	}
	for _, entry := range strings.Split(*override, ";") {
		if strings.EqualFold(strings.TrimSpace(entry), "<local>") {
			return *override
		}
	}
	return strings.TrimRight(*override, ";") + ";<local>"
}

func writeState(h syscall.Handle, w *wininetState) error {
	if w == nil {
		w = &wininetState{}
	}
	var errs []error
	put := func(value string, s *string) {
		if s == nil {
			errs = append(errs, deleteValue(h, value))
		} else {
			errs = append(errs, setString(h, value, *s))
		}
	}
	put("ProxyServer", w.ProxyServer)
	put("ProxyOverride", w.ProxyOverride)
	if w.ProxyEnable == nil {
		errs = append(errs, deleteValue(h, "ProxyEnable"))
	} else {
		errs = append(errs, setDWORD(h, "ProxyEnable", *w.ProxyEnable))
	}
	return errors.Join(errs...)
}

func explain(err error, what string) error {
	if errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
		return fmt.Errorf("Windows did not let FormNET %s: this user may not change the Internet Settings (access denied)", what)
	}
	return fmt.Errorf("FormNET could not %s: %w", what, err)
}

func applySystem(addr string, cur snapshot) error {
	h, err := openKey(syscall.KEY_QUERY_VALUE | syscall.KEY_SET_VALUE)
	if err != nil {
		return explain(err, "change the proxy, so nothing was changed")
	}
	defer syscall.RegCloseKey(h)
	prev := cur.WinINET
	if prev == nil {
		prev = &wininetState{}
	}
	err = setString(h, "ProxyServer", "http="+addr+";https="+addr)
	if err == nil {
		err = setString(h, "ProxyOverride", withLocal(prev.ProxyOverride))
	}
	if err == nil {
		err = setDWORD(h, "ProxyEnable", 1)
	}
	if err != nil {
		_ = writeState(h, prev)
		notify()
		return explain(err, "change the proxy, so it was left as it was")
	}
	notify()
	return nil
}

func restoreSystem(prev, cur snapshot, addr string) error {
	if !pointsAt(cur, addr) {
		return nil
	}
	h, err := openKey(syscall.KEY_QUERY_VALUE | syscall.KEY_SET_VALUE)
	if err != nil {
		return explain(err, "put the previous proxy back")
	}
	defer syscall.RegCloseKey(h)
	err = writeState(h, prev.WinINET)
	notify()
	if err != nil {
		return explain(err, "put the previous proxy back")
	}
	return nil
}
