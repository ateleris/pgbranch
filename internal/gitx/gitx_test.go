package gitx

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tempDir returns t.TempDir() with symlinks and Windows short names resolved,
// so it compares equal to the absolute paths git reports (/private/var on
// macOS, RUNNER~1 vs runneradmin on Windows).
func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return dir
}

// runGit runs git in dir and fails the test on error.
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "git %v: %s", args, out)
	return string(out)
}

// newTestRepo creates a temp git repo with one commit on "main".
func newTestRepo(t *testing.T) string {
	t.Helper()
	dir := tempDir(t)
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.name", "Test User")
	runGit(t, dir, "config", "user.email", "test@example.com")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hello"), 0644))
	runGit(t, dir, "add", "file.txt")
	runGit(t, dir, "commit", "-m", "initial commit")

	return dir
}

func TestOpen(t *testing.T) {
	dir := newTestRepo(t)

	repo, err := Open(dir)
	require.NoError(t, err)
	assert.Equal(t, dir, repo.Dir)
}

func TestOpen_NotAGitRepo(t *testing.T) {
	dir := tempDir(t)

	_, err := Open(dir)
	assert.Error(t, err)
}

func TestTopLevel(t *testing.T) {
	dir := newTestRepo(t)
	repo, err := Open(dir)
	require.NoError(t, err)

	top, err := repo.TopLevel()
	require.NoError(t, err)

	wantTop, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	gotTop, err := filepath.EvalSymlinks(top)
	require.NoError(t, err)
	assert.Equal(t, wantTop, gotTop)
}

func TestCommonDirAndGitDir_MainWorktree(t *testing.T) {
	dir := newTestRepo(t)
	repo, err := Open(dir)
	require.NoError(t, err)

	commonDir, err := repo.CommonDir()
	require.NoError(t, err)
	gitDir, err := repo.GitDir()
	require.NoError(t, err)

	assert.Equal(t, commonDir, gitDir, "main worktree: git-dir should equal common-dir")
	assert.Equal(t, filepath.Join(dir, ".git"), commonDir)

	linked, err := repo.IsLinkedWorktree()
	require.NoError(t, err)
	assert.False(t, linked)

	root, err := repo.MainWorktreeRoot()
	require.NoError(t, err)
	wantRoot, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	gotRoot, err := filepath.EvalSymlinks(root)
	require.NoError(t, err)
	assert.Equal(t, wantRoot, gotRoot)
}

func TestLinkedWorktree(t *testing.T) {
	dir := newTestRepo(t)

	worktreeParent := tempDir(t)
	worktreeDir := filepath.Join(worktreeParent, "linked")
	runGit(t, dir, "worktree", "add", "-b", "wt-branch", worktreeDir)

	repo, err := Open(worktreeDir)
	require.NoError(t, err)

	linked, err := repo.IsLinkedWorktree()
	require.NoError(t, err)
	assert.True(t, linked)

	commonDir, err := repo.CommonDir()
	require.NoError(t, err)
	gitDir, err := repo.GitDir()
	require.NoError(t, err)
	assert.NotEqual(t, commonDir, gitDir)
	assert.Equal(t, filepath.Join(dir, ".git"), commonDir)

	root, err := repo.MainWorktreeRoot()
	require.NoError(t, err)
	wantRoot, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	gotRoot, err := filepath.EvalSymlinks(root)
	require.NoError(t, err)
	assert.Equal(t, wantRoot, gotRoot)

	branch, err := repo.CurrentBranch()
	require.NoError(t, err)
	assert.Equal(t, "wt-branch", branch)

	// hooks dir for the linked worktree must be the common (main) repo's hooks dir.
	hooksDir, err := repo.HooksDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(commonDir, "hooks"), hooksDir)
}

func TestHooksDir_Default(t *testing.T) {
	dir := newTestRepo(t)
	repo, err := Open(dir)
	require.NoError(t, err)

	hooksDir, err := repo.HooksDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, ".git", "hooks"), hooksDir)
}

func TestHooksDir_CoreHooksPathAbsolute(t *testing.T) {
	dir := newTestRepo(t)
	customHooks := tempDir(t)
	runGit(t, dir, "config", "core.hooksPath", customHooks)

	repo, err := Open(dir)
	require.NoError(t, err)

	hooksDir, err := repo.HooksDir()
	require.NoError(t, err)
	assert.Equal(t, customHooks, hooksDir)
}

func TestHooksDir_CoreHooksPathRelative(t *testing.T) {
	dir := newTestRepo(t)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "custom-hooks"), 0755))
	runGit(t, dir, "config", "core.hooksPath", "custom-hooks")

	repo, err := Open(dir)
	require.NoError(t, err)

	hooksDir, err := repo.HooksDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "custom-hooks"), hooksDir)
}

func TestCurrentBranch(t *testing.T) {
	dir := newTestRepo(t)
	repo, err := Open(dir)
	require.NoError(t, err)

	branch, err := repo.CurrentBranch()
	require.NoError(t, err)
	assert.Equal(t, "main", branch)
}

func TestCurrentBranch_Detached(t *testing.T) {
	dir := newTestRepo(t)
	runGit(t, dir, "checkout", "--detach", "HEAD")

	repo, err := Open(dir)
	require.NoError(t, err)

	branch, err := repo.CurrentBranch()
	require.NoError(t, err)
	assert.Equal(t, "", branch)
}

// TestCurrentBranch_TagWithSameNameAsBranch reproduces a bug in
// `git rev-parse --abbrev-ref HEAD`: when a tag exists with the same name
// as the checked-out branch, it disambiguates by returning "heads/<branch>"
// instead of just "<branch>".
func TestCurrentBranch_TagWithSameNameAsBranch(t *testing.T) {
	dir := newTestRepo(t)
	runGit(t, dir, "tag", "main")

	repo, err := Open(dir)
	require.NoError(t, err)

	branch, err := repo.CurrentBranch()
	require.NoError(t, err)
	assert.Equal(t, "main", branch, "must return the plain branch name even when a same-named tag exists")
}

// TestLocalBranches_TagWithSameNameAsBranch reproduces a bug in
// `%(refname:short)`: when a tag exists with the same name as a branch, the
// for-each-ref format prints "heads/<branch>" for the branch entry to
// disambiguate it from the tag, instead of the plain branch name.
func TestLocalBranches_TagWithSameNameAsBranch(t *testing.T) {
	dir := newTestRepo(t)
	runGit(t, dir, "branch", "feature-1")
	runGit(t, dir, "tag", "feature-1")

	repo, err := Open(dir)
	require.NoError(t, err)

	branches, err := repo.LocalBranches()
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"main", "feature-1"}, branches)
}

func TestLocalBranchesAndBranchExists(t *testing.T) {
	dir := newTestRepo(t)
	runGit(t, dir, "branch", "feature-1")
	runGit(t, dir, "branch", "feature-2")

	repo, err := Open(dir)
	require.NoError(t, err)

	branches, err := repo.LocalBranches()
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"main", "feature-1", "feature-2"}, branches)

	exists, err := repo.BranchExists("feature-1")
	require.NoError(t, err)
	assert.True(t, exists)

	exists, err = repo.BranchExists("does-not-exist")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestOperationInProgress_None(t *testing.T) {
	dir := newTestRepo(t)
	repo, err := Open(dir)
	require.NoError(t, err)

	inProgress, err := repo.OperationInProgress()
	require.NoError(t, err)
	assert.False(t, inProgress)
}

func TestOperationInProgress_Rebase(t *testing.T) {
	dir := newTestRepo(t)

	// Create a branch that conflicts with main so rebase stops with a conflict.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file.txt"), []byte("main change"), 0644))
	runGit(t, dir, "commit", "-am", "change on main")

	runGit(t, dir, "checkout", "-b", "conflict-branch", "HEAD~1")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file.txt"), []byte("conflicting change"), 0644))
	runGit(t, dir, "commit", "-am", "conflicting change")

	// Rebase onto main; expect a conflict, so don't fail the test on git's error exit.
	cmd := exec.Command("git", "rebase", "main")
	cmd.Dir = dir
	_ = cmd.Run()

	repo, err := Open(dir)
	require.NoError(t, err)

	inProgress, err := repo.OperationInProgress()
	require.NoError(t, err)
	assert.True(t, inProgress)

	// clean up so t.TempDir removal doesn't get confused by rebase state.
	abortCmd := exec.Command("git", "rebase", "--abort")
	abortCmd.Dir = dir
	_ = abortCmd.Run()
}
