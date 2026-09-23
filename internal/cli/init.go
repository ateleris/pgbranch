package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/le-vlad/pgbranch/internal/credentials"
	"github.com/le-vlad/pgbranch/internal/gitx"
	"github.com/le-vlad/pgbranch/internal/hooks"
	"github.com/le-vlad/pgbranch/pkg/config"
	"github.com/le-vlad/pgbranch/pkg/core"
)

var (
	initDatabases   []string
	initStrategy    string
	initHost        string
	initPort        int
	initUser        string
	initPassword    string
	initBaseline    string
	initInstallHook bool
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize pgbranch for one or more databases",
	Long: `Initialize pgbranch in the current directory (or, inside a git
repository, at its top level).

This creates a .pgbranch directory to store configuration,
metadata, and database snapshots.

Example:
  pgbranch init -d myapp_dev
  pgbranch init -d app -d app_identity:dump --baseline main --hook
  pgbranch init -d myapp_dev -H localhost -p 5432 -U postgres`,
	RunE: runInit,
}

func init() {
	initCmd.Flags().StringArrayVarP(&initDatabases, "database", "d", nil, "Database name (repeatable); optionally 'name:strategy' (auto|template|dump)")
	initCmd.Flags().StringVar(&initStrategy, "strategy", "", "Default clone strategy for databases without one (auto|template|dump)")
	initCmd.Flags().StringVarP(&initHost, "host", "H", "localhost", "PostgreSQL host")
	initCmd.Flags().IntVarP(&initPort, "port", "p", 5432, "PostgreSQL port")
	initCmd.Flags().StringVarP(&initUser, "user", "U", "postgres", "PostgreSQL user")
	initCmd.Flags().StringVarP(&initPassword, "password", "W", "", "PostgreSQL password (stored in plain text; prefer PGPASSWORD or ~/.pgpass)")
	initCmd.Flags().StringVar(&initBaseline, "baseline", "main", "Baseline branch name")
	initCmd.Flags().BoolVar(&initInstallHook, "hook", false, "Install the post-checkout git hook")
	_ = initCmd.MarkFlagRequired("database")
}

func runInit(cmd *cobra.Command, args []string) error {
	cwd, err := config.WorkingDir()
	if err != nil {
		return err
	}

	dir, err := workspaceRoot(cwd)
	if err != nil {
		return err
	}

	if config.IsInitialized(dir) {
		return fmt.Errorf("pgbranch already initialized in %s", dir)
	}

	databases, err := parseInitDatabases(initDatabases, initStrategy)
	if err != nil {
		return err
	}

	if initPassword != "" {
		yellow := color.New(color.FgYellow).SprintFunc()
		fmt.Printf("%s -W stores the password in plain text in .pgbranch/config.json.\n", yellow("!"))
		fmt.Println("  Prefer PGPASSWORD or a ~/.pgpass entry instead.")
	}

	cfg := &config.Config{
		Databases:      databases,
		Host:           initHost,
		Port:           initPort,
		User:           initUser,
		Password:       initPassword,
		BaselineBranch: initBaseline,
	}

	if err := core.Initialize(dir, cfg); err != nil {
		return err
	}

	green := color.New(color.FgGreen).SprintFunc()
	names := make([]string, len(databases))
	for i, db := range databases {
		names[i] = db.Name
	}
	fmt.Printf("%s Initialized pgbranch for database(s): %s\n", green("✓"), strings.Join(names, ", "))

	if err := addToGitExclude(dir); err != nil {
		yellow := color.New(color.FgYellow).SprintFunc()
		fmt.Printf("%s Could not update .git/info/exclude: %v\n", yellow("!"), err)
	}

	if !credentials.KeyExists() {
		keyPath, _ := credentials.GetKeyPath()
		_, _, err := credentials.EnsureKey()
		if err != nil {
			fmt.Printf("\nWarning: failed to generate encryption key: %v\n", err)
		} else {
			fmt.Printf("%s Generated encryption key at %s\n", green("✓"), keyPath)
		}
	}

	brancher, err := core.Open(dir)
	if err == nil {
		gitBranch := ""
		if repo, gerr := gitx.Open(dir); gerr == nil {
			if cb, cerr := repo.CurrentBranch(); cerr == nil {
				gitBranch = cb
			}
		}

		toCreate, current := initialBranches(initBaseline, gitBranch)

		allOK := true
		for _, name := range toCreate {
			if createErr := brancher.CreateBranch(cmd.Context(), name, ""); createErr == nil {
				fmt.Printf("%s Created branch '%s'\n", green("✓"), name)
			} else {
				allOK = false
				yellow := color.New(color.FgYellow).SprintFunc()
				fmt.Printf("%s Could not create branch '%s' automatically: %v\n", yellow("!"), name, createErr)
			}
		}

		if allOK {
			brancher.Metadata.CurrentBranch = current
			_ = brancher.Metadata.Save()
		}
	}

	if initInstallHook {
		if err := installHook(); err != nil {
			yellow := color.New(color.FgYellow).SprintFunc()
			fmt.Printf("%s Could not install git hook: %v\n", yellow("!"), err)
		}
	}

	fmt.Println("\nNext steps:")
	if !initInstallHook {
		fmt.Println("  pgbranch hook install   # keep database branches in sync with git branches")
	}
	fmt.Println("  pgbranch branch         # list branches")
	fmt.Println("  pgbranch status         # show current state")

	return nil
}

// parseInitDatabases builds config.DatabaseConfig entries from repeatable
// -d flags, each either "name" or "name:strategy". defaultStrategy fills in
// entries with no strategy of their own.
func parseInitDatabases(specs []string, defaultStrategy string) ([]config.DatabaseConfig, error) {
	if len(specs) == 0 {
		return nil, fmt.Errorf("at least one -d/--database is required")
	}

	databases := make([]config.DatabaseConfig, 0, len(specs))
	seen := make(map[string]bool, len(specs))

	for _, spec := range specs {
		name, strategy := spec, ""
		if idx := strings.Index(spec, ":"); idx >= 0 {
			name, strategy = spec[:idx], spec[idx+1:]
		}
		if name == "" {
			return nil, fmt.Errorf("invalid -d/--database value %q", spec)
		}
		if strategy == "" {
			strategy = defaultStrategy
		}
		if seen[name] {
			return nil, fmt.Errorf("duplicate database name %q", name)
		}
		seen[name] = true

		databases = append(databases, config.DatabaseConfig{Name: name, Strategy: strategy})
	}

	return databases, nil
}

// initialBranches decides, from the configured baseline branch and the git
// branch checked out when init runs (empty when not in a git repository or
// HEAD is detached), which branch(es) init should snapshot the working
// databases into and which one becomes current. The baseline branch is
// always snapshotted; if the current git branch differs from it, that
// branch is snapshotted too (from the same working state) and becomes
// current, so init does not silently record the working databases under
// the wrong git branch.
func initialBranches(baseline, gitBranch string) (toCreate []string, current string) {
	if gitBranch == "" || gitBranch == baseline {
		return []string{baseline}, baseline
	}
	return []string{baseline, gitBranch}, gitBranch
}

// addToGitExclude adds .pgbranch/ to the repository's .git/info/exclude
// (the common git dir, so it applies to all worktrees) if dir is inside a
// git repository and .pgbranch is not already ignored.
func addToGitExclude(dir string) error {
	repo, err := gitx.Open(dir)
	if err != nil {
		// Not a git repository; nothing to do.
		return nil
	}

	checkCmd := exec.Command("git", "check-ignore", "-q", ".pgbranch")
	checkCmd.Dir = dir
	if err := checkCmd.Run(); err == nil {
		// Already ignored.
		return nil
	}

	commonDir, err := repo.CommonDir()
	if err != nil {
		return err
	}

	excludePath := filepath.Join(commonDir, "info", "exclude")
	if err := os.MkdirAll(filepath.Dir(excludePath), 0o755); err != nil {
		return err
	}

	f, err := os.OpenFile(excludePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	_, err = f.WriteString("\n.pgbranch/\n")
	return err
}

func installHook() error {
	dir, err := config.WorkingDir()
	if err != nil {
		return err
	}
	repo, err := gitx.Open(dir)
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
		fmt.Printf("%s Installed git hook at %s\n", green("✓"), result.Path)
	case hooks.Appended:
		fmt.Printf("%s Added pgbranch to existing hook at %s\n", green("✓"), result.Path)
	case hooks.AlreadyInstalled:
		fmt.Printf("%s Git hook already installed\n", green("✓"))
	case hooks.ManualRequired:
		fmt.Println(result.Instructions)
	}
	return nil
}
