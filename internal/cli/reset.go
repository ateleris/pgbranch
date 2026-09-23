package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var (
	resetFrom    string
	resetConfirm bool
)

var resetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Reset the working databases to a branch's snapshot",
	Long: `Recreate the working databases from a branch's snapshot (the
baseline branch by default), discarding all current state, then
overwrite the current branch's snapshot with the result.

Example:
  pgbranch reset
  pgbranch reset --from main -y`,
	RunE: runReset,
}

func init() {
	resetCmd.Flags().StringVar(&resetFrom, "from", "", "Branch to reset from (default: the baseline branch)")
	resetCmd.Flags().BoolVarP(&resetConfirm, "yes", "y", false, "Skip the confirmation prompt")
	rootCmd.AddCommand(resetCmd)
}

func runReset(cmd *cobra.Command, args []string) error {
	brancher, err := openBrancher()
	if err != nil {
		return err
	}

	from := resetFrom
	if from == "" {
		from = brancher.Config.BaselineBranchOrDefault()
	}

	if !resetConfirm {
		red := color.New(color.FgRed, color.Bold).SprintFunc()
		fmt.Printf("%s This will discard all current changes and reset the working database(s) to '%s'.\n", red("!"), from)
		fmt.Print("Continue? [y/N]: ")

		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	l, err := acquireLock()
	if err != nil {
		return err
	}
	defer func() { _ = l.Release() }()

	if err := brancher.Reset(cmd.Context(), resetFrom); err != nil {
		return err
	}

	green := color.New(color.FgGreen).SprintFunc()
	fmt.Printf("%s Reset working database(s) to '%s'\n", green("✓"), from)

	return nil
}
