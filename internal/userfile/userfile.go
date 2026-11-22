// Package userfile writes files only the current user can read: mode 0600 on
// Unix, and on Windows an access list that grants the current user alone,
// with inheritance from the folder turned off.
package userfile

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
)

// Write replaces path with data atomically: a private temporary file in the
// same folder, renamed over the old one.
func Write(path string, data []byte) error {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return err
	}
	dir, base := filepath.Split(path)
	tmp := filepath.Join(dir, "."+base+"."+hex.EncodeToString(b[:])+".tmp")
	f, err := Create(tmp)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
	}
	return err
}
