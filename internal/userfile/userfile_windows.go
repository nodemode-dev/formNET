//go:build windows

package userfile

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

var (
	advapi32                 = syscall.NewLazyDLL("advapi32.dll")
	procStringToSecurityDesc = advapi32.NewProc("ConvertStringSecurityDescriptorToSecurityDescriptorW")
	procSecurityDescToString = advapi32.NewProc("ConvertSecurityDescriptorToStringSecurityDescriptorW")
	procGetNamedSecurityInfo = advapi32.NewProc("GetNamedSecurityInfoW")
	errNoSecurityDescriptor  = errors.New("no security descriptor")
	allowedSIDs              = map[string]bool{"SY": true, "S-1-5-18": true, "BA": true, "S-1-5-32-544": true, "OW": true, "S-1-3-4": true}
	readRights               = map[string]bool{"FA": true, "FR": true, "GA": true, "GR": true, "CC": true}
)

const (
	sddlRevision     = 1
	seFileObject     = 1
	daclSecurityInfo = 4
	readMask         = 0x1 | 0x10000000 | 0x80000000
)

func currentUserSID() (string, error) {
	tok, err := syscall.OpenCurrentProcessToken()
	if err != nil {
		return "", err
	}
	defer tok.Close()
	u, err := tok.GetTokenUser()
	if err != nil {
		return "", err
	}
	return u.User.Sid.String()
}

// Create makes a new file at path whose access list grants the current user
// full control and nobody else anything, failing if the file already exists.
func Create(path string) (*os.File, error) {
	sid, err := currentUserSID()
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	sddl, err := syscall.UTF16PtrFromString("D:P(A;;FA;;;" + sid + ")")
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	var sd uintptr
	ok, _, callErr := procStringToSecurityDesc.Call(uintptr(unsafe.Pointer(sddl)), sddlRevision, uintptr(unsafe.Pointer(&sd)), 0)
	if ok == 0 {
		return nil, &os.PathError{Op: "open", Path: path, Err: callErr}
	}
	defer syscall.LocalFree(syscall.Handle(sd))
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	sa := &syscall.SecurityAttributes{Length: uint32(unsafe.Sizeof(syscall.SecurityAttributes{})), SecurityDescriptor: sd}
	h, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, sa, syscall.CREATE_NEW, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}

func accessList(path string) (string, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	var sd uintptr
	rc, _, _ := procGetNamedSecurityInfo.Call(uintptr(unsafe.Pointer(name)), seFileObject, daclSecurityInfo, 0, 0, 0, 0, uintptr(unsafe.Pointer(&sd)))
	if rc != 0 {
		return "", syscall.Errno(rc)
	}
	if sd == 0 {
		return "", errNoSecurityDescriptor
	}
	defer syscall.LocalFree(syscall.Handle(sd))
	var str *uint16
	var n uint32
	ok, _, callErr := procSecurityDescToString.Call(sd, sddlRevision, daclSecurityInfo, uintptr(unsafe.Pointer(&str)), uintptr(unsafe.Pointer(&n)))
	if ok == 0 {
		return "", callErr
	}
	defer syscall.LocalFree(syscall.Handle(unsafe.Pointer(str)))
	return syscall.UTF16ToString(unsafe.Slice(str, n)), nil
}

// Check returns an error, with the command that fixes it, when someone other
// than the current user, SYSTEM or the Administrators can read path.
func Check(path string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	sid, err := currentUserSID()
	if err != nil {
		return fmt.Errorf("%s: reading the current user: %w", path, err)
	}
	sddl, err := accessList(path)
	if err != nil {
		return fmt.Errorf("%s: reading its access list: %w", path, err)
	}
	others, explicit := readers(sddl, sid)
	if len(others) == 0 {
		return nil
	}
	fix := `icacls "` + path + `" /inheritance:r /grant:r "*` + sid + `:F"`
	if len(explicit) > 0 {
		fix += " /remove:g"
		for _, s := range explicit {
			fix += " *" + s
		}
	}
	return fmt.Errorf("%s can be read by other users of this computer (%s); fix it in a Command Prompt with: %s", path, strings.Join(others, ", "), fix)
}

// readers lists the accounts other than sid, SYSTEM, the Administrators and
// the owner that an access list in SDDL lets read, and which of them hold an
// entry of their own rather than one inherited from the folder.
func readers(sddl, sid string) (others, explicit []string) {
	dacl := sddl
	if i := strings.Index(dacl, "D:"); i >= 0 {
		dacl = dacl[i+2:]
	}
	if strings.HasPrefix(dacl, "NO_ACCESS_CONTROL") {
		return []string{"everyone"}, nil
	}
	for _, ace := range strings.Split(dacl, "(")[1:] {
		end := strings.Index(ace, ")")
		if end < 0 {
			continue
		}
		parts := strings.Split(ace[:end], ";")
		if len(parts) < 6 {
			continue
		}
		kind, flags, rights, who := parts[0], parts[1], parts[2], parts[5]
		if !strings.HasSuffix(kind, "A") || strings.Contains(flags, "IO") || !readable(rights) {
			continue
		}
		if who == sid || allowedSIDs[who] {
			continue
		}
		full := who
		if s, err := syscall.StringToSid(who); err == nil {
			if str, err := s.String(); err == nil {
				full = str
			}
		}
		if full == sid || allowedSIDs[full] {
			continue
		}
		others = append(others, full)
		if !strings.Contains(flags, "ID") {
			explicit = append(explicit, full)
		}
	}
	return others, explicit
}

func readable(rights string) bool {
	if hexText, ok := strings.CutPrefix(strings.ToLower(rights), "0x"); ok {
		n, err := strconv.ParseUint(hexText, 16, 32)
		return err != nil || n&readMask != 0
	}
	for i := 0; i+2 <= len(rights); i += 2 {
		if readRights[rights[i:i+2]] {
			return true
		}
	}
	return false
}
