package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/le-vlad/pgbranch/internal/testutil"
	"github.com/le-vlad/pgbranch/pkg/config"
	"github.com/le-vlad/pgbranch/pkg/storage"
)

// buildPgbranch builds the pgbranch binary for these tests and returns the
// directory containing it, so it can be prepended to PATH for the hook
// script to find via `command -v pgbranch`.
func buildPgbranch(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")

	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "pgbranch")

	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/pgbranch")
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "go build failed: %s", out)

	return binDir
}

func runGitEnv(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return string(out)
}

// TestHookSwitchesDatabaseBranch is an end-to-end test: it builds the real
// pgbranch binary, initializes a workspace with two databases and the git
// hook in a throwaway git repository, then runs `git checkout -b feature`
// for real and asserts the post-checkout hook created and switched to a
// matching database branch.
func TestHookSwitchesDatabaseBranch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	pg, err := testutil.StartPostgresContainer(ctx)
	require.NoError(t, err)
	defer func() { _ = pg.Stop(ctx) }()

	baseCfg := pg.GetConfig()

	adminConn, err := pgx.Connect(ctx, baseCfg.ConnectionURLForDB("postgres"))
	require.NoError(t, err)
	_, err = adminConn.Exec(ctx, "CREATE DATABASE a")
	require.NoError(t, err)
	_, err = adminConn.Exec(ctx, "CREATE DATABASE b")
	require.NoError(t, err)
	require.NoError(t, adminConn.Close(ctx))

	binDir := buildPgbranch(t)

	repoDir := t.TempDir()
	env := append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)

	runGitEnv(t, repoDir, env, "init", "-q", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("hello\n"), 0o644))
	runGitEnv(t, repoDir, env, "add", "README.md")
	runGitEnv(t, repoDir, env, "commit", "-q", "-m", "init")

	initCmd := exec.Command(filepath.Join(binDir, "pgbranch"),
		"init",
		"-d", "a", "-d", "b",
		"--baseline", "main",
		"--hook",
		"-H", baseCfg.Host,
		"-p", strconv.Itoa(baseCfg.Port),
		"-U", baseCfg.User,
		"-W", baseCfg.Password,
	)
	initCmd.Dir = repoDir
	initCmd.Env = env
	out, err := initCmd.CombinedOutput()
	require.NoError(t, err, "pgbranch init failed: %s", out)

	hookPath := filepath.Join(repoDir, ".git", "hooks", "post-checkout")
	_, err = os.Stat(hookPath)
	require.NoError(t, err, "post-checkout hook was not installed")

	// The real git hook runs here: this is the moment the hook, running the
	// binary we built above via PATH, creates and switches to the "feature"
	// database branch.
	out2 := runGitEnv(t, repoDir, env, "checkout", "-b", "feature")
	t.Logf("git checkout -b feature output:\n%s", out2)

	meta, err := storage.LoadMetadata(repoDir)
	require.NoError(t, err)

	require.True(t, meta.BranchExists("feature"), "hook did not create the 'feature' database branch")
	require.Equal(t, "feature", meta.CurrentBranch, "hook did not switch to the 'feature' database branch")

	cfg, err := config.Load(repoDir)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"a", "b"}, cfg.DatabaseNames())
}

// TestInitOnNonBaselineBranchTracksGitBranch is an end-to-end test: it runs
// `pgbranch init` while git is already checked out to a branch other than
// the configured baseline. init must snapshot the working databases as both
// the baseline branch and the current git branch, and set the current
// database branch to the git branch -- not silently record the working
// state under "main" while git says "develop".
func TestInitOnNonBaselineBranchTracksGitBranch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	pg, err := testutil.StartPostgresContainer(ctx)
	require.NoError(t, err)
	defer func() { _ = pg.Stop(ctx) }()

	baseCfg := pg.GetConfig()

	adminConn, err := pgx.Connect(ctx, baseCfg.ConnectionURLForDB("postgres"))
	require.NoError(t, err)
	_, err = adminConn.Exec(ctx, "CREATE DATABASE a")
	require.NoError(t, err)
	require.NoError(t, adminConn.Close(ctx))

	binDir := buildPgbranch(t)

	repoDir := t.TempDir()
	env := append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)

	runGitEnv(t, repoDir, env, "init", "-q", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("hello\n"), 0o644))
	runGitEnv(t, repoDir, env, "add", "README.md")
	runGitEnv(t, repoDir, env, "commit", "-q", "-m", "init")
	runGitEnv(t, repoDir, env, "checkout", "-q", "-b", "develop")

	initCmd := exec.Command(filepath.Join(binDir, "pgbranch"),
		"init",
		"-d", "a",
		"--baseline", "main",
		"-H", baseCfg.Host,
		"-p", strconv.Itoa(baseCfg.Port),
		"-U", baseCfg.User,
		"-W", baseCfg.Password,
	)
	initCmd.Dir = repoDir
	initCmd.Env = env
	out, err := initCmd.CombinedOutput()
	require.NoError(t, err, "pgbranch init failed: %s", out)

	meta, err := storage.LoadMetadata(repoDir)
	require.NoError(t, err)

	require.True(t, meta.BranchExists("main"), "init did not create the baseline 'main' database branch")
	require.True(t, meta.BranchExists("develop"), "init did not create a 'develop' database branch matching the checked-out git branch")
	require.Equal(t, "develop", meta.CurrentBranch, "init must set the current database branch to the checked-out git branch, not the baseline")
}
