//go:build windows

package ca

import (
	"fmt"

	"github.com/nodemode-dev/formnet/internal/userfile"
)

// checkKeyMode refuses a key file that other users on this computer could read.
func checkKeyMode(path string) error {
	if err := userfile.Check(path); err != nil {
		return fmt.Errorf("ca: %w", err)
	}
	return nil
}
