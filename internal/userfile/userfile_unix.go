//go:build !windows

package userfile

import "os"

// Create makes a new file at path that only the current user can read,
// failing if the file already exists.
func Create(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
}
