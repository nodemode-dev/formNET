//go:build windows

package ca

import "os"

// checkKeyMode only confirms the file exists; Windows carries no Unix mode bits.
func checkKeyMode(path string) error {
	_, err := os.Stat(path)
	return err
}
