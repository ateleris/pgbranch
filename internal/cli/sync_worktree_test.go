package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/le-vlad/pgbranch/pkg/config"
	"github.com/le-vlad/pgbranch/pkg/core"
	"github.com/le-vlad/pgbranch/pkg/storage"
)

// chdir changes the process working directory for the duration of the test
// and restores it afterwards. sync.go's git checks must use this directory,
// not the .pgbranch workspace directory.
func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

// TestDoSync_FromLinkedWorktree_UsesWorktreeGitState reproduces the bug
// where sync.go opened gitx on the .pgbranch workspace directory (the main
// worktree, since .pgbranch is not per-worktree) instead of the process's
// actual working directory. That made every git check (current branch,
// linked-worktree detection, operation-in-progress) describe the main
// worktree even when pgbranch was invoked from a linked worktree checked
// out to a completely different branch.
//
// Setup: the main worktree is on branch "shared" (unrelated, no matching DB
// branch). The linked worktree is checked out to "develop", which matches
// the already-current DB branch. follow_worktrees defaults to false, so
// sync run from the linked worktree must recognize it is in a linked
// worktree and no-op.
//
// If sync instead reads git state from the main worktree (the bug), it
// sees gitBranch "shared" (not a linked worktree from that vantage point),
// which does not match the current DB branch "develop", so it attempts to
// create and check out a "shared" database branch -- reaching out to
// Postgres over an address that cannot be resolved, which fails loudly
// rather than silently doing the wrong thing.
func TestDoSync_FromLinkedWorktree_UsesWorktreeGitState(t *testing.T) {
	mainDir := t.TempDir()
	runGit(t, mainDir, "init", "-q", "-b", "shared")
	if err := os.WriteFile(filepath.Join(mainDir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, mainDir, "add", "f.txt")
	runGit(t, mainDir, "commit", "-q", "-m", "init")

	// .pgbranch lives only in the main worktree. Host/port are deliberately
	// unreachable so that any attempt to actually touch a database fails
	// fast and loudly, instead of the test depending on a running Postgres.
	cfg := &config.Config{
		Databases:      []config.DatabaseConfig{{Name: "app"}},
		Host:           "pgbranch-test-unreachable.invalid",
		Port:           1,
		User:           "postgres",
		BaselineBranch: "develop",
	}
	if err := core.Initialize(mainDir, cfg); err != nil {
		t.Fatal(err)
	}

	meta, err := storage.LoadMetadata(mainDir)
	if err != nil {
		t.Fatal(err)
	}
	meta.AddBranch("develop", "", map[string]string{"app": "app_pgbranch_develop"})
	meta.CurrentBranch = "develop"
	if err := meta.Save(); err != nil {
		t.Fatal(err)
	}

	// A linked worktree checked out to "develop", matching the DB's current
	// branch -- if sync correctly reads *its* git state, it is a no-op.
	worktreeParent := t.TempDir()
	worktreeDir := filepath.Join(worktreeParent, "feature-wt")
	runGit(t, mainDir, "worktree", "add", "-q", "-b", "develop", worktreeDir)

	chdir(t, worktreeDir)

	err = doSync(context.Background(), "", "", "1", false)
	if err != nil {
		t.Fatalf("doSync from linked worktree must no-op, not reach out to Postgres: %v", err)
	}

	metaAfter, err := storage.LoadMetadata(mainDir)
	if err != nil {
		t.Fatal(err)
	}
	if metaAfter.CurrentBranch != "develop" {
		t.Errorf("sync from a linked worktree must be a no-op; current branch changed to %q", metaAfter.CurrentBranch)
	}
	if metaAfter.BranchExists("shared") {
		t.Errorf("sync from a linked worktree must not create a database branch for the main worktree's git branch")
	}
}
