package cli

import (
	"fmt"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/le-vlad/pgbranch/internal/gitx"
	"github.com/le-vlad/pgbranch/pkg/postgres"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current branch and status",
	Long: `Show the current branch and repository status.

Example:
  pgbranch status`,
	RunE: runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
	brancher, err := openBrancher()
	if err != nil {
		return err
	}

	cfg := brancher.Config

	green := color.New(color.FgGreen).SprintFunc()
	cyan := color.New(color.FgCyan).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()

	fmt.Printf("Host: %s:%d\n\n", cfg.Host, cfg.Port)

	currentBranch, branchCount := brancher.Status()

	if currentBranch == "" {
		fmt.Printf("DB branch:  %s\n", yellow("(none)"))
	} else {
		fmt.Printf("DB branch:  %s\n", green(currentBranch))
	}
	fmt.Printf("Branches:   %d\n", branchCount)

	dir, err := workspace()
	if err == nil {
		if repo, err := gitx.Open(dir); err == nil {
			if gitBranch, err := repo.CurrentBranch(); err == nil && gitBranch != "" {
				fmt.Printf("Git branch: %s\n", gitBranch)
				if currentBranch != "" && gitBranch != currentBranch {
					fmt.Printf("%s git branch and database branch differ\n", yellow("!"))
				}
			}
		}
	}

	fmt.Println()
	fmt.Println("Databases:")

	ctx := cmd.Context()
	for _, db := range cfg.Databases {
		strategy, err := brancher.Client.ResolveStrategy(ctx, db.Name, mustParseStrategy(db.Strategy))
		strategyLabel := string(strategy)
		if err != nil {
			strategyLabel = fmt.Sprintf("%s (unresolved: %v)", db.Strategy, err)
		}

		fmt.Printf("  %s\n", cyan(db.Name))
		fmt.Printf("    strategy: %s\n", strategyLabel)

		var snapshot string
		if currentBranch != "" {
			if branch, ok := brancher.Metadata.GetBranch(currentBranch); ok {
				snapshot = branch.SnapshotFor(db.Name)
			}
		}
		if snapshot != "" {
			exists, err := brancher.Client.Exists(ctx, snapshot)
			existsLabel := "unknown"
			if err == nil {
				existsLabel = fmt.Sprintf("%v", exists)
			}
			fmt.Printf("    snapshot: %s (exists: %s)\n", snapshot, existsLabel)
		}

		if size, err := brancher.Client.Size(ctx, db.Name); err == nil {
			fmt.Printf("    size:     %s\n", formatSize(size))
		}
	}

	return nil
}

func mustParseStrategy(s string) postgres.Strategy {
	strategy, err := postgres.ParseStrategy(s)
	if err != nil {
		return postgres.StrategyAuto
	}
	return strategy
}
