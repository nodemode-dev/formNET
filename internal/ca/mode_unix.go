//go:build !windows

package ca

import (
	"fmt"
	"os"
)

// checkKeyMode refuses a key file that other users on this computer could read.
func checkKeyMode(path string) error {
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if st.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("ca: %s is readable by other users (mode %04o); fix it with: chmod 600 %s", path, st.Mode().Perm(), shellQuote(path))
	}
	return nil
}
