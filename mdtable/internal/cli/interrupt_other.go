//go:build !windows

package cli

func inputInterrupted(e error) bool { return false }
