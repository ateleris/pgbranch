package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/le-vlad/pgbranch/pkg/config"
)

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func initPgbranch(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(config.RootDir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestFindWorkspaceFindsPgbranchDirDirectly(t *testing.T) {
	dir := t.TempDir()
	initPgbranch(t, dir)

	got, err := findWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != dir {
		t.Errorf("got %q, want %q", got, dir)
	}
}

func TestFindWorkspaceWalksUpToParent(t *testing.T) {
	dir := t.TempDir()
	initPgbranch(t, dir)

	sub := filepath.Join(dir, "a", "b", "c")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := findWorkspace(sub)
	if err != nil {
		t.Fatal(err)
	}
	if got != dir {
		t.Errorf("got %q, want %q", got, dir)
	}
}

func TestFindWorkspaceFallsBackToGitTopLevel(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	initPgbranch(t, dir)

	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := findWorkspace(sub)
	if err != nil {
		t.Fatal(err)
	}
	if resolved, _ := filepath.EvalSymlinks(got); resolved != mustEvalSymlinks(t, dir) {
		t.Errorf("got %q, want %q", got, dir)
	}
}

func TestFindWorkspaceFallsBackToMainWorktreeRoot(t *testing.T) {
	main := t.TempDir()
	runGit(t, main, "init", "-q")
	runGit(t, main, "config", "user.email", "test@test.com")
	runGit(t, main, "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(main, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, main, "add", "f")
	runGit(t, main, "commit", "-q", "-m", "init")

	initPgbranch(t, main)

	worktree := filepath.Join(filepath.Dir(main), filepath.Base(main)+"-worktree")
	runGit(t, main, "worktree", "add", worktree, "-b", "linked")

	got, err := findWorkspace(worktree)
	if err != nil {
		t.Fatal(err)
	}
	if resolved, _ := filepath.EvalSymlinks(got); resolved != mustEvalSymlinks(t, main) {
		t.Errorf("got %q, want %q", got, main)
	}
}

func TestFindWorkspaceFallsBackToStartWhenNothingFound(t *testing.T) {
	dir := t.TempDir()

	got, err := findWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != dir {
		t.Errorf("got %q, want %q", got, dir)
	}
}

func TestWorkspaceRootIsGitTopLevelInsideRepo(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")

	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := workspaceRoot(sub)
	if err != nil {
		t.Fatal(err)
	}
	if resolved, _ := filepath.EvalSymlinks(got); resolved != mustEvalSymlinks(t, dir) {
		t.Errorf("got %q, want %q", got, dir)
	}
}

func TestWorkspaceRootIsStartOutsideGit(t *testing.T) {
	dir := t.TempDir()

	got, err := workspaceRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != dir {
		t.Errorf("got %q, want %q", got, dir)
	}
}

func mustEvalSymlinks(t *testing.T, p string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}
