//go:build windows

package lock

import "os"

// processAlive reports whether pid identifies a running process.
// os.FindProcess on Windows actually opens a handle to the process, so a
// success/failure result is meaningful (unlike on Unix, where FindProcess
// always succeeds and the real check is the signal below it).
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// os.Process has no portable liveness check on Windows beyond Wait,
	// which only works on child processes; Release just frees the handle
	// pgbranch itself opened, it doesn't affect the other process.
	_ = proc.Release()
	return true
}
