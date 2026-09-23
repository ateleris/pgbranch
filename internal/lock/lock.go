// Package lock provides a simple, cross-platform advisory lock file used to
// serialize pgbranch's mutating operations (checkout, branch, delete,
// prune, reset, sync) against concurrent invocations -- e.g. two shells
// running `pgbranch sync` at once, or an editor's git hook firing while a
// manual command is still running -- which would otherwise race on the
// same working databases and snapshots.
//
// It uses an O_EXCL lock file holding the owning process's PID, with stale
// (dead-process) lock detection, rather than OS-specific file locking
// syscalls (flock(2), LockFileEx, ...).
package lock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// FileName is the name of the lock file inside the .pgbranch directory.
const FileName = "lock"

// ErrLocked is returned by Acquire when another live pgbranch process
// already holds the lock.
var ErrLocked = errors.New("another pgbranch operation is already in progress (.pgbranch/lock is held)")

// Lock represents a held lock file. Release must be called to release it.
type Lock struct {
	path string
}

// Acquire acquires the lock in workspaceRootDir (a .pgbranch directory),
// writing this process's PID to the lock file. If the lock is already held
// by a live process, it returns ErrLocked. If it is held by a PID that is
// no longer running (stale -- e.g. left behind by a process that crashed),
// the stale lock is removed and acquisition is retried once.
func Acquire(workspaceRootDir string) (*Lock, error) {
	path := filepath.Join(workspaceRootDir, FileName)

	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_, writeErr := fmt.Fprintf(f, "%d\n", os.Getpid())
			closeErr := f.Close()
			if writeErr != nil || closeErr != nil {
				_ = os.Remove(path)
				return nil, fmt.Errorf("failed to write lock file: %w", errors.Join(writeErr, closeErr))
			}
			return &Lock{path: path}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("failed to acquire lock: %w", err)
		}

		if pid, perr := readPID(path); perr == nil && !processAlive(pid) {
			// Stale lock left behind by a process that no longer exists;
			// clean it up and retry once.
			_ = os.Remove(path)
			continue
		}
		return nil, ErrLocked
	}

	return nil, ErrLocked
}

// Release releases the lock, removing the lock file.
func (l *Lock) Release() error {
	return os.Remove(l.path)
}

func readPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}
