//go:build unix && !linux && !darwin

package terminal

func members(sid int) []int { return nil }
