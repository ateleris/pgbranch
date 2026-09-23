package lock

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

func TestAcquireAndRelease(t *testing.T) {
	dir := t.TempDir()

	l, err := Acquire(dir)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, FileName)); err != nil {
		t.Fatalf("lock file not created: %v", err)
	}

	if err := l.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, FileName)); !os.IsNotExist(err) {
		t.Fatalf("lock file still present after Release")
	}
}

func TestAcquireFailsWhenAlreadyHeldByLiveProcess(t *testing.T) {
	dir := t.TempDir()

	l, err := Acquire(dir)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer func() { _ = l.Release() }()

	_, err = Acquire(dir)
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("second Acquire = %v, want ErrLocked", err)
	}
}

// TestAcquireRemovesStaleLockFromDeadProcess reproduces the crash-recovery
// case: a lock file left behind by a process that no longer exists must be
// cleaned up and re-acquired, not treated as held forever.
func TestAcquireRemovesStaleLockFromDeadProcess(t *testing.T) {
	dir := t.TempDir()

	// Spawn a trivial short-lived process and wait for it to exit, so we
	// have a PID that is guaranteed not to be running any more.
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatalf("spawning helper process: %v", err)
	}
	deadPID := cmd.Process.Pid

	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(strconv.Itoa(deadPID)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	l, err := Acquire(dir)
	if err != nil {
		t.Fatalf("Acquire over a stale lock: %v", err)
	}
	defer func() { _ = l.Release() }()

	pid, err := readPID(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if pid != os.Getpid() {
		t.Fatalf("lock file has pid %d, want this process's pid %d", pid, os.Getpid())
	}
}

func TestAcquireGarbageLockFileIsTreatedAsHeld(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("not-a-pid"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Acquire(dir)
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("Acquire over garbage lock file = %v, want ErrLocked (fail closed)", err)
	}
}
