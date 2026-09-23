package cli

import (
	"fmt"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/le-vlad/pgbranch/internal/gitx"
	"github.com/le-vlad/pgbranch/internal/hooks"
	"github.com/le-vlad/pgbranch/pkg/config"
)

var hookCmd = &cobra.Command{
	Use:   "hook",
	Short: "Manage git hooks for automatic branch switching",
	Long: `Manage git hooks that automatically switch database branches
when you switch git branches.

Subcommands:
  install   - Install the post-checkout git hook
  uninstall - Remove the post-checkout git hook`,
}

var hookInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install git hook for automatic branch switching",
	Long: `Install a post-checkout git hook that runs 'pgbranch sync --hook'
when you switch git branches, keeping database branches in sync.

Example:
  pgbranch hook install
  git checkout feature-x  # automatically syncs the database branch`,
	RunE: runHookInstall,
}

var hookUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove the git hook",
	Long: `Remove the post-checkout git hook installed by pgbranch.

Example:
  pgbranch hook uninstall`,
	RunE: runHookUninstall,
}

func init() {
	hookCmd.AddCommand(hookInstallCmd)
	hookCmd.AddCommand(hookUninstallCmd)
}

func openRepoForHooks() (*gitx.Repo, error) {
	dir, err := config.WorkingDir()
	if err != nil {
		return nil, err
	}
	return gitx.Open(dir)
}

func runHookInstall(cmd *cobra.Command, args []string) error {
	repo, err := openRepoForHooks()
	if err != nil {
		return err
	}

	result, err := hooks.Install(repo)
	if err != nil {
		return err
	}

	green := color.New(color.FgGreen).SprintFunc()
	switch result.Status {
	case hooks.Installed:
		fmt.Printf("%s Git hook installed at %s\n", green("✓"), result.Path)
		fmt.Println()
		fmt.Println("Now when you run 'git checkout <branch>', pgbranch will")
		fmt.Println("automatically switch (or create) the matching database branch.")
	case hooks.Appended:
		fmt.Printf("%s Added pgbranch to the existing hook at %s\n", green("✓"), result.Path)
	case hooks.AlreadyInstalled:
		fmt.Println("pgbranch hook is already installed")
	case hooks.ManualRequired:
		fmt.Println(result.Instructions)
	}

	return nil
}

func runHookUninstall(cmd *cobra.Command, args []string) error {
	repo, err := openRepoForHooks()
	if err != nil {
		return err
	}

	result, err := hooks.Uninstall(repo)
	if err != nil {
		return err
	}

	green := color.New(color.FgGreen).SprintFunc()
	switch result.Status {
	case hooks.Removed:
		fmt.Printf("%s Git hook removed (%s)\n", green("✓"), result.Path)
	case hooks.LinesRemoved:
		fmt.Printf("%s Removed pgbranch from %s\n", green("✓"), result.Path)
	case hooks.NotInstalled:
		fmt.Println("No pgbranch hook found")
	}

	return nil
}
