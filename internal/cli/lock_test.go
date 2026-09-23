package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/le-vlad/pgbranch/internal/lock"
	"github.com/le-vlad/pgbranch/pkg/config"
	"github.com/le-vlad/pgbranch/pkg/core"
	"github.com/le-vlad/pgbranch/pkg/storage"
)

// TestDoSync_HookMode_LockHeld_IsANoOp verifies that when another pgbranch
// operation holds the workspace lock, a hook-invoked sync does not error
// (which would fail the user's `git checkout`) but simply skips.
func TestDoSync_HookMode_LockHeld_IsANoOp(t *testing.T) {
	mainDir := t.TempDir()
	runGit(t, mainDir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(mainDir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, mainDir, "add", "f.txt")
	runGit(t, mainDir, "commit", "-q", "-m", "init")
	runGit(t, mainDir, "checkout", "-q", "-b", "feature")

	cfg := &config.Config{
		Databases:      []config.DatabaseConfig{{Name: "app"}},
		Host:           "localhost",
		Port:           5432,
		User:           "postgres",
		BaselineBranch: "main",
	}
	if err := core.Initialize(mainDir, cfg); err != nil {
		t.Fatal(err)
	}

	meta, err := storage.LoadMetadata(mainDir)
	if err != nil {
		t.Fatal(err)
	}
	meta.AddBranch("main", "", map[string]string{"app": "app_pgbranch_main"})
	meta.CurrentBranch = "main"
	if err := meta.Save(); err != nil {
		t.Fatal(err)
	}

	chdir(t, mainDir)

	l, err := lock.Acquire(config.RootDir(mainDir))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Release() }()

	// gitBranch "feature" differs from the current DB branch "main", so
	// without the lock this would attempt to create/checkout a branch --
	// reaching out to Postgres. With the lock held, it must skip instead.
	if err := doSync(context.Background(), "", "", "1", true); err != nil {
		t.Fatalf("doSync in hook mode with the lock held must not error: %v", err)
	}

	metaAfter, err := storage.LoadMetadata(mainDir)
	if err != nil {
		t.Fatal(err)
	}
	if metaAfter.CurrentBranch != "main" {
		t.Errorf("sync must have been skipped while the lock was held; current branch changed to %q", metaAfter.CurrentBranch)
	}
	if metaAfter.BranchExists("feature") {
		t.Errorf("sync must have been skipped while the lock was held; a 'feature' branch was created")
	}
}

// TestDoSync_NonHookMode_LockHeld_ReturnsError verifies that a manually
// invoked `pgbranch sync` (not the hook) surfaces an error when the lock is
// held, rather than silently doing nothing.
func TestDoSync_NonHookMode_LockHeld_ReturnsError(t *testing.T) {
	mainDir := t.TempDir()
	runGit(t, mainDir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(mainDir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, mainDir, "add", "f.txt")
	runGit(t, mainDir, "commit", "-q", "-m", "init")

	cfg := &config.Config{
		Databases:      []config.DatabaseConfig{{Name: "app"}},
		Host:           "localhost",
		Port:           5432,
		User:           "postgres",
		BaselineBranch: "main",
	}
	if err := core.Initialize(mainDir, cfg); err != nil {
		t.Fatal(err)
	}

	meta, err := storage.LoadMetadata(mainDir)
	if err != nil {
		t.Fatal(err)
	}
	meta.AddBranch("main", "", map[string]string{"app": "app_pgbranch_main"})
	meta.CurrentBranch = "main"
	if err := meta.Save(); err != nil {
		t.Fatal(err)
	}

	chdir(t, mainDir)

	l, err := lock.Acquire(config.RootDir(mainDir))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Release() }()

	if err := doSync(context.Background(), "", "", "1", false); err == nil {
		t.Fatalf("doSync outside hook mode with the lock held must return an error")
	}
}
