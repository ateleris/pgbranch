package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/le-vlad/pgbranch/internal/lock"
	"github.com/le-vlad/pgbranch/pkg/config"
	"github.com/le-vlad/pgbranch/pkg/core"
)

var rootCmd = &cobra.Command{
	Use:   "pgbranch",
	Short: "Git-style branching for PostgreSQL databases",
	Long: `pgbranch - A CLI tool for managing PostgreSQL database branches.

Create, switch, and manage database snapshots just like git branches.
Perfect for local development when you need to work with different
database states.

Example workflow:
  pgbranch init -d myapp_dev
  pgbranch branch main
  pgbranch branch feature-x
  pgbranch checkout main
  pgbranch delete feature-x

Share snapshots with your team:
  pgbranch remote add origin /shared/snapshots
  pgbranch push main
  pgbranch pull main`,
}

func Execute() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}

// workspace returns the directory the CLI operates on: the nearest
// .pgbranch directory found by walking up from the current working
// directory, falling back to the git top-level directory or main worktree
// root, and finally the current working directory itself. Library callers
// pass their workspace directory explicitly.
func workspace() (string, error) {
	cwd, err := config.WorkingDir()
	if err != nil {
		return "", err
	}
	return findWorkspace(cwd)
}

// openBrancher opens the pgbranch workspace in the current working directory.
func openBrancher() (*core.Brancher, error) {
	dir, err := workspace()
	if err != nil {
		return nil, err
	}

	brancher, err := core.Open(dir)
	if err != nil {
		return nil, notInitializedHint(err)
	}
	return brancher, nil
}

// acquireLock acquires the workspace's lock file, to serialize a mutating
// operation (checkout, branch create, delete, prune, reset, sync) against
// concurrent pgbranch invocations. Callers must release it (typically via
// defer) once the operation is done.
func acquireLock() (*lock.Lock, error) {
	dir, err := workspace()
	if err != nil {
		return nil, err
	}

	l, err := lock.Acquire(config.RootDir(dir))
	if err != nil {
		if errors.Is(err, lock.ErrLocked) {
			return nil, fmt.Errorf("%w. If you're sure no other pgbranch command is running, remove %s", err, lock.FileName)
		}
		return nil, err
	}
	return l, nil
}

// loadConfig loads the configuration from the current working directory.
func loadConfig() (*config.Config, error) {
	dir, err := workspace()
	if err != nil {
		return nil, err
	}

	cfg, err := config.Load(dir)
	if err != nil {
		return nil, notInitializedHint(err)
	}
	return cfg, nil
}

// notInitializedHint replaces the library's "not initialized in <dir>" error
// with the CLI's own wording, which names the command to run. pkg/config has
// no business telling an embedder to run 'pgbranch init'.
func notInitializedHint(err error) error {
	if errors.Is(err, config.ErrNotInitialized) {
		return errors.New("pgbranch not initialized. Run 'pgbranch init' first")
	}
	return err
}

func init() {
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(branchCmd)
	rootCmd.AddCommand(checkoutCmd)
	rootCmd.AddCommand(deleteCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(logCmd)
	rootCmd.AddCommand(hookCmd)
	rootCmd.AddCommand(pruneCmd)
	rootCmd.AddCommand(updateCmd)

	rootCmd.AddCommand(newRemoteCmd())
	rootCmd.AddCommand(newPushCmd())
	rootCmd.AddCommand(newPullCmd())
	rootCmd.AddCommand(newKeysCmd())
	rootCmd.AddCommand(newMigrateCmd())
}
