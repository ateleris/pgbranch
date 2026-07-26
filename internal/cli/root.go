package cli

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

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

// workspace returns the directory the CLI operates on: the current working
// directory. Library callers pass their workspace directory explicitly.
func workspace() (string, error) {
	return config.WorkingDir()
}

// openBrancher opens the pgbranch workspace in the current working directory.
func openBrancher() (*core.Brancher, error) {
	dir, err := workspace()
	if err != nil {
		return nil, err
	}
	return core.Open(dir)
}

// loadConfig loads the configuration from the current working directory.
func loadConfig() (*config.Config, error) {
	dir, err := workspace()
	if err != nil {
		return nil, err
	}
	return config.Load(dir)
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
