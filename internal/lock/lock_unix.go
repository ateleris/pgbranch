//go:build !windows

package lock

import (
	"os"
	"syscall"
)

// processAlive reports whether pid identifies a running process, by sending
// it signal 0 (which performs the existence/permission check without
// actually delivering a signal).
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}
