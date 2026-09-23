package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/le-vlad/pgbranch/internal/gitx"
	"github.com/le-vlad/pgbranch/pkg/config"
	"github.com/le-vlad/pgbranch/pkg/core"
)

var syncHookMode bool

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync the database branch with the current git branch",
	Long: `Sync the database branch with the current git branch: checks out
the matching DB branch, creating it first if it doesn't exist yet.

Run standalone, or installed as the post-checkout git hook via
'pgbranch hook install' (which calls 'pgbranch sync --hook "$@"').

Example:
  pgbranch sync
  pgbranch sync --hook <prev-head> <new-head> <flag>`,
	Args: cobra.MaximumNArgs(3),
	RunE: runSync,
}

func init() {
	syncCmd.Flags().BoolVar(&syncHookMode, "hook", false, "internal: invoked by the post-checkout git hook")
	rootCmd.AddCommand(syncCmd)
}

func runSync(cmd *cobra.Command, args []string) error {
	if syncHookMode {
		var prev, next, flag string
		if len(args) > 0 {
			prev = args[0]
		}
		if len(args) > 1 {
			next = args[1]
		}
		if len(args) > 2 {
			flag = args[2]
		}
		if err := doSync(cmd.Context(), prev, next, flag, true); err != nil {
			fmt.Fprintf(os.Stderr, "pgbranch: %v\n", err)
		}
		return nil
	}

	return doSync(cmd.Context(), "", "", "1", false)
}

// syncDecisionInput holds every input to the pure decision of whether sync
// should run. Keeping it separate from git/filesystem access lets the
// decision be unit tested without a repository.
type syncDecisionInput struct {
	hookMode            bool
	flag                string
	prevHEAD            string
	newHEAD             string
	gitBranch           string // "" means detached HEAD
	dbCurrentBranch     string
	linkedWorktree      bool
	operationInProgress bool
	followWorktrees     bool
}

// shouldSync decides whether a sync should run, given the hook's arguments
// and the repository's state. It never touches the filesystem.
func shouldSync(in syncDecisionInput) bool {
	if in.hookMode && in.flag != "1" {
		return false
	}
	if in.gitBranch == "" {
		return false
	}
	if in.operationInProgress {
		return false
	}
	if in.linkedWorktree && !in.followWorktrees {
		return false
	}
	if in.hookMode && in.prevHEAD != "" && in.prevHEAD == in.newHEAD && in.gitBranch == in.dbCurrentBranch {
		return false
	}
	return true
}

func doSync(ctx context.Context, prevHEAD, newHEAD, flag string, hookMode bool) error {
	dir, err := workspace()
	if err != nil {
		return err
	}

	if !config.IsInitialized(dir) {
		return nil
	}

	repo, err := gitx.Open(dir)
	if err != nil {
		return nil
	}

	gitBranch, err := repo.CurrentBranch()
	if err != nil {
		return err
	}

	linked, err := repo.IsLinkedWorktree()
	if err != nil {
		return err
	}

	opInProgress, err := repo.OperationInProgress()
	if err != nil {
		return err
	}

	cfg, err := config.Load(dir)
	if err != nil {
		return notInitializedHint(err)
	}

	brancher, err := core.Open(dir)
	if err != nil {
		return notInitializedHint(err)
	}

	if !shouldSync(syncDecisionInput{
		hookMode:            hookMode,
		flag:                flag,
		prevHEAD:            prevHEAD,
		newHEAD:             newHEAD,
		gitBranch:           gitBranch,
		dbCurrentBranch:     brancher.Metadata.CurrentBranch,
		linkedWorktree:      linked,
		operationInProgress: opInProgress,
		followWorktrees:     cfg.FollowWorktrees,
	}) {
		return nil
	}

	existed := brancher.Metadata.BranchExists(gitBranch)

	if err := brancher.Sync(ctx, gitBranch); err != nil {
		return err
	}

	if existed {
		fmt.Printf("pgbranch: switched database to branch '%s'\n", gitBranch)
	} else {
		fmt.Printf("pgbranch: created and switched database to branch '%s'\n", gitBranch)
	}

	return nil
}
