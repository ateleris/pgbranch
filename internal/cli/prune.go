package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/le-vlad/pgbranch/internal/gitx"
	"github.com/le-vlad/pgbranch/pkg/core"
)

var (
	pruneDays   int
	pruneForce  bool
	pruneGone   bool
	pruneDryRun bool
)

var pruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Remove stale branches",
	Long: `Remove branches that haven't been accessed in a specified number of days.

By default, shows an interactive list of stale branches where you can
deselect branches you want to keep.

Use --force (-y) to skip interactive mode and prune all stale branches.
Use --days (-d) to customize the stale threshold (default: 7 days).

Examples:
  pgbranch prune              # Interactive mode
  pgbranch prune -y           # Prune all stale branches without confirmation
  pgbranch prune -d 14        # Consider branches stale after 14 days
  pgbranch prune -d 14 -y     # Prune all branches older than 14 days`,
	RunE: runPrune,
}

func init() {
	pruneCmd.Flags().IntVarP(&pruneDays, "days", "d", core.DefaultStaleDays, "Days after which a branch is considered stale")
	pruneCmd.Flags().BoolVarP(&pruneForce, "force", "y", false, "Skip interactive mode and prune all stale branches")
	pruneCmd.Flags().BoolVar(&pruneGone, "gone", false, "Prune branches whose git branch no longer exists locally")
	pruneCmd.Flags().BoolVar(&pruneDryRun, "dry-run", false, "Show what would be pruned without deleting anything")
}

func runPrune(cmd *cobra.Command, args []string) error {
	brancher, err := openBrancher()
	if err != nil {
		return err
	}

	if pruneGone {
		return runPruneGone(cmd, brancher)
	}

	staleBranches := brancher.GetStaleBranches(pruneDays)

	if len(staleBranches) == 0 {
		green := color.New(color.FgGreen).SprintFunc()
		fmt.Printf("%s No stale branches found (threshold: %d days).\n", green("✓"), pruneDays)
		return nil
	}

	yellow := color.New(color.FgYellow).SprintFunc()
	cyan := color.New(color.FgCyan).SprintFunc()
	dim := color.New(color.Faint).SprintFunc()

	fmt.Printf("%s Found %d stale branch(es) (not accessed in %d+ days):\n\n",
		yellow("!"), len(staleBranches), pruneDays)

	for i, info := range staleBranches {
		days := info.Branch.DaysSinceLastAccess()
		var lastAccess string
		if info.Branch.LastCheckoutAt.IsZero() {
			lastAccess = "never checked out"
		} else {
			lastAccess = fmt.Sprintf("last checkout: %s", info.Branch.LastCheckoutAt.Format("2006-01-02"))
		}

		currentMarker := ""
		if info.IsCurrent {
			currentMarker = cyan(" (current)")
		}

		fmt.Printf("  %d. %s%s\n", i+1, info.Name, currentMarker)
		fmt.Printf("     %s | %s | %s\n",
			dim(fmt.Sprintf("created: %s", info.Branch.CreatedAt.Format("2006-01-02"))),
			dim(lastAccess),
			dim(fmt.Sprintf("%d days ago", days)),
		)
	}
	fmt.Println()

	var toPrune []string

	if pruneForce {
		for _, info := range staleBranches {
			toPrune = append(toPrune, info.Name)
		}
	} else {
		toPrune, err = interactiveSelect(staleBranches)
		if err != nil {
			return err
		}
	}

	if len(toPrune) == 0 {
		fmt.Println("No branches selected for pruning.")
		return nil
	}

	if !pruneForce {
		red := color.New(color.FgRed, color.Bold).SprintFunc()
		fmt.Printf("\n%s This will permanently delete %d branch(es) and their database snapshots.\n",
			red("!"), len(toPrune))
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

	fmt.Println()
	deleted, errors := brancher.PruneBranches(cmd.Context(), toPrune)

	green := color.New(color.FgGreen).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()

	for _, name := range deleted {
		fmt.Printf("%s Deleted branch '%s'\n", green("✓"), name)
	}

	for _, err := range errors {
		fmt.Printf("%s %v\n", red("✗"), err)
	}

	if len(deleted) > 0 {
		fmt.Printf("\n%s Pruned %d branch(es).\n", green("✓"), len(deleted))
	}

	return nil
}

func runPruneGone(cmd *cobra.Command, brancher *core.Brancher) error {
	dir, err := workspace()
	if err != nil {
		return err
	}

	repo, err := gitx.Open(dir)
	if err != nil {
		return fmt.Errorf("prune --gone requires a git repository: %w", err)
	}

	localBranches, err := repo.LocalBranches()
	if err != nil {
		return err
	}

	gone := brancher.GoneBranches(localBranches)

	if len(gone) == 0 {
		green := color.New(color.FgGreen).SprintFunc()
		fmt.Printf("%s No gone branches found.\n", green("✓"))
		return nil
	}

	yellow := color.New(color.FgYellow).SprintFunc()
	fmt.Printf("%s Found %d branch(es) whose git branch no longer exists:\n\n", yellow("!"), len(gone))
	for _, name := range gone {
		fmt.Printf("  %s\n", name)
	}
	fmt.Println()

	if pruneDryRun {
		fmt.Println("Dry run: nothing was deleted.")
		return nil
	}

	if !pruneForce {
		red := color.New(color.FgRed, color.Bold).SprintFunc()
		fmt.Printf("%s This will permanently delete %d branch(es) and their database snapshots.\n",
			red("!"), len(gone))
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

	deleted, errs := brancher.PruneBranches(cmd.Context(), gone)

	green := color.New(color.FgGreen).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()

	for _, name := range deleted {
		fmt.Printf("%s Deleted branch '%s'\n", green("✓"), name)
	}
	for _, err := range errs {
		fmt.Printf("%s %v\n", red("✗"), err)
	}

	if len(deleted) > 0 {
		fmt.Printf("\n%s Pruned %d branch(es).\n", green("✓"), len(deleted))
	}

	return nil
}

func interactiveSelect(branches []core.BranchInfo) ([]string, error) {
	if len(branches) == 0 {
		return nil, nil
	}

	cyan := color.New(color.FgCyan).SprintFunc()
	dim := color.New(color.Faint).SprintFunc()

	fmt.Println("Enter branch numbers to KEEP (comma-separated), or press Enter to prune all:")
	fmt.Printf("  %s\n", dim("Example: 1,3 (keeps branches 1 and 3, prunes the rest)"))
	fmt.Print("> ")

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read input: %w", err)
	}

	input = strings.TrimSpace(input)

	if input == "" {
		result := make([]string, len(branches))
		for i, info := range branches {
			result[i] = info.Name
		}
		return result, nil
	}

	keepSet := make(map[int]bool)
	parts := strings.Split(input, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		var num int
		if _, err := fmt.Sscanf(part, "%d", &num); err != nil {
			fmt.Printf("  %s Invalid number: %s (ignored)\n", cyan("?"), part)
			continue
		}
		if num < 1 || num > len(branches) {
			fmt.Printf("  %s Number out of range: %d (ignored)\n", cyan("?"), num)
			continue
		}
		keepSet[num] = true
	}

	var toPrune []string
	for i, info := range branches {
		if !keepSet[i+1] {
			toPrune = append(toPrune, info.Name)
		}
	}

	return toPrune, nil
}
