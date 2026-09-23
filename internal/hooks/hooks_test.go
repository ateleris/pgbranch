package hooks

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/le-vlad/pgbranch/internal/gitx"
)

// oldForkHook mirrors the previous fork's post-checkout hook well enough to
// be recognized: it contains the marker comment and calls "pgbranch checkout".
const oldForkHook = `#!/bin/sh
# pgbranch post-checkout hook
# Automatically switches database branch when git branch changes
BRANCH=$(git rev-parse --abbrev-ref HEAD)
if [ "$BRANCH" = "HEAD" ]; then
    exit 0
fi
if pgbranch branch 2>/dev/null | grep -q "^[* ] $BRANCH$"; then
    pgbranch checkout "$BRANCH" 2>/dev/null
fi
`

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "git %v: %s", args, out)
	return string(out)
}

func newTestRepo(t *testing.T) (*gitx.Repo, string) {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.name", "Test User")
	runGit(t, dir, "config", "user.email", "test@example.com")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hello"), 0644))
	runGit(t, dir, "add", "file.txt")
	runGit(t, dir, "commit", "-m", "initial commit")

	repo, err := gitx.Open(dir)
	require.NoError(t, err)
	return repo, dir
}

func hookPath(t *testing.T, repo *gitx.Repo) string {
	t.Helper()
	hooksDir, err := repo.HooksDir()
	require.NoError(t, err)
	return filepath.Join(hooksDir, "post-checkout")
}

func TestInstall_Fresh(t *testing.T) {
	repo, _ := newTestRepo(t)

	result, err := Install(repo)
	require.NoError(t, err)
	assert.Equal(t, Installed, result.Status)

	content, err := os.ReadFile(result.Path)
	require.NoError(t, err)
	assert.Equal(t, Script(), string(content))

	info, err := os.Stat(result.Path)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode().Perm()&0o111, "hook should be executable")

	installed, err := IsInstalled(repo)
	require.NoError(t, err)
	assert.True(t, installed)
}

func TestInstall_IdempotentReinstall(t *testing.T) {
	repo, _ := newTestRepo(t)

	_, err := Install(repo)
	require.NoError(t, err)

	result, err := Install(repo)
	require.NoError(t, err)
	assert.Equal(t, AlreadyInstalled, result.Status)

	content, err := os.ReadFile(result.Path)
	require.NoError(t, err)
	assert.Equal(t, Script(), string(content))
}

func TestInstall_AppendsToExistingForeignHook(t *testing.T) {
	repo, _ := newTestRepo(t)

	path := hookPath(t, repo)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	foreign := "#!/bin/sh\necho custom hook\n"
	require.NoError(t, os.WriteFile(path, []byte(foreign), 0o755))

	result, err := Install(repo)
	require.NoError(t, err)
	assert.Equal(t, Appended, result.Status)

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(content), foreign), "original content preserved")
	assert.Contains(t, string(content), GuardLine)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode().Perm()&0o111, "hook should remain executable")

	// re-install must be idempotent: no duplicate guard line.
	result2, err := Install(repo)
	require.NoError(t, err)
	assert.Equal(t, AlreadyInstalled, result2.Status)

	content2, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(content2), GuardLine))
}

func TestInstall_ReplacesOldForkHook(t *testing.T) {
	repo, _ := newTestRepo(t)

	path := hookPath(t, repo)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(oldForkHook), 0o755))

	result, err := Install(repo)
	require.NoError(t, err)
	assert.Equal(t, Installed, result.Status)

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, Script(), string(content))
}

func TestUninstall_RemovesOurScript(t *testing.T) {
	repo, _ := newTestRepo(t)

	_, err := Install(repo)
	require.NoError(t, err)

	result, err := Uninstall(repo)
	require.NoError(t, err)
	assert.Equal(t, Removed, result.Status)

	_, err = os.Stat(result.Path)
	assert.True(t, os.IsNotExist(err))
}

func TestUninstall_RemovesOldForkHook(t *testing.T) {
	repo, _ := newTestRepo(t)

	path := hookPath(t, repo)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(oldForkHook), 0o755))

	result, err := Uninstall(repo)
	require.NoError(t, err)
	assert.Equal(t, Removed, result.Status)

	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err))
}

func TestUninstall_RemovesOnlyAppendedLines(t *testing.T) {
	repo, _ := newTestRepo(t)

	path := hookPath(t, repo)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	foreign := "#!/bin/sh\necho custom hook\n"
	require.NoError(t, os.WriteFile(path, []byte(foreign), 0o755))

	_, err := Install(repo)
	require.NoError(t, err)

	result, err := Uninstall(repo)
	require.NoError(t, err)
	assert.Equal(t, LinesRemoved, result.Status)

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, foreign, string(content))
}

func TestUninstall_NotInstalled(t *testing.T) {
	repo, _ := newTestRepo(t)

	result, err := Uninstall(repo)
	require.NoError(t, err)
	assert.Equal(t, NotInstalled, result.Status)
}

func TestInstall_HuskyDetected(t *testing.T) {
	repo, dir := newTestRepo(t)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".husky"), 0o755))

	result, err := Install(repo)
	require.NoError(t, err)
	assert.Equal(t, ManualRequired, result.Status)
	assert.Contains(t, result.Instructions, GuardLine)

	installed, err := IsInstalled(repo)
	require.NoError(t, err)
	assert.False(t, installed)
}

func TestInstall_LefthookDetected(t *testing.T) {
	repo, dir := newTestRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "lefthook.yml"), []byte("post-checkout:\n"), 0o644))

	result, err := Install(repo)
	require.NoError(t, err)
	assert.Equal(t, ManualRequired, result.Status)
	assert.Contains(t, result.Instructions, GuardLine)
}

func TestInstall_LinkedWorktreeUsesCommonHooksDir(t *testing.T) {
	_, dir := newTestRepo(t)

	worktreeParent := t.TempDir()
	worktreeDir := filepath.Join(worktreeParent, "linked")
	runGit(t, dir, "worktree", "add", "-b", "wt-branch", worktreeDir)

	linkedRepo, err := gitx.Open(worktreeDir)
	require.NoError(t, err)

	result, err := Install(linkedRepo)
	require.NoError(t, err)
	assert.Equal(t, Installed, result.Status)
	assert.Equal(t, filepath.Join(dir, ".git", "hooks", "post-checkout"), result.Path)
}

func TestInstall_CoreHooksPathHonored(t *testing.T) {
	repo, dir := newTestRepo(t)
	customHooks := filepath.Join(dir, "custom-hooks")
	require.NoError(t, os.MkdirAll(customHooks, 0o755))
	runGit(t, dir, "config", "core.hooksPath", "custom-hooks")

	result, err := Install(repo)
	require.NoError(t, err)
	assert.Equal(t, Installed, result.Status)
	assert.Equal(t, filepath.Join(customHooks, "post-checkout"), result.Path)
}

// TestInstalledHookInvokesPgbranchSync actually runs `git checkout` with a
// fake pgbranch binary on PATH and asserts the installed hook invokes
// `pgbranch sync --hook <prev> <new> 1`.
func TestInstalledHookInvokesPgbranchSync(t *testing.T) {
	repo, dir := newTestRepo(t)

	result, err := Install(repo)
	require.NoError(t, err)
	require.Equal(t, Installed, result.Status)

	binDir := t.TempDir()
	argsFile := filepath.Join(binDir, "args.txt")
	fakeScript := "#!/bin/sh\necho \"$@\" > " + argsFile + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "pgbranch"), []byte(fakeScript), 0o755))

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	prevSHA := strings.TrimSpace(runGit(t, dir, "rev-parse", "HEAD"))
	runGit(t, dir, "checkout", "-b", "feature")
	newSHA := strings.TrimSpace(runGit(t, dir, "rev-parse", "HEAD"))

	recorded, err := os.ReadFile(argsFile)
	require.NoError(t, err)
	assert.Equal(t, "sync --hook "+prevSHA+" "+newSHA+" 1\n", string(recorded))
}
