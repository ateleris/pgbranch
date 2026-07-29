package cli

import (
	"errors"
	"fmt"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/le-vlad/pgbranch/pkg/core"
)

var deleteForce bool

var deleteCmd = &cobra.Command{
	Use:     "delete <branch>",
	Aliases: []string{"rm"},
	Short:   "Delete a branch",
	Long: `Delete a branch and its snapshot.

Cannot delete the current branch unless --force is used.

Example:
  pgbranch delete feature-x
  pgbranch delete main --force`,
	Args: cobra.ExactArgs(1),
	RunE: runDelete,
}

func init() {
	deleteCmd.Flags().BoolVarP(&deleteForce, "force", "f", false, "Force delete even if current branch")
}

func runDelete(cmd *cobra.Command, args []string) error {
	brancher, err := openBrancher()
	if err != nil {
		return err
	}

	name := args[0]

	if err := brancher.DeleteBranch(cmd.Context(), name, deleteForce); err != nil {
		// pkg/core reports the cause without naming a CLI flag; point the
		// user at --force here, where the flag actually exists.
		var branchErr *core.BranchError
		if errors.As(err, &branchErr) && errors.Is(err, core.ErrCurrentBranch) {
			return fmt.Errorf("cannot delete current branch '%s'. Use --force to override", branchErr.Name)
		}
		return err
	}

	green := color.New(color.FgGreen).SprintFunc()
	fmt.Printf("%s Deleted branch '%s'\n", green("✓"), name)

	return nil
}
