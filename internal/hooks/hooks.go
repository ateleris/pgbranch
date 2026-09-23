// Package hooks installs and removes the pgbranch post-checkout git hook,
// which keeps the database branch in sync with the git branch.
package hooks

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/le-vlad/pgbranch/internal/gitx"
)

// GuardLine is the shell snippet that invokes pgbranch when it is
// available. It is safe to append to any existing hook script.
const GuardLine = `command -v pgbranch >/dev/null 2>&1 && pgbranch sync --hook "$@"`

// marker identifies a post-checkout hook file as ours.
const marker = "# pgbranch post-checkout hook"

// oldForkMarker additionally appears in the previous fork's hook script,
// which called "pgbranch checkout" directly instead of "pgbranch sync".
const oldForkMarker = "pgbranch checkout"

// Script returns the full POSIX sh post-checkout hook script pgbranch
// installs when no hook exists yet.
func Script() string {
	return "#!/bin/sh\n" + marker + "\n" + GuardLine + "\n"
}

// Status describes the outcome of an Install or Uninstall call.
type Status int

const (
	// Installed means pgbranch wrote a fresh hook script (or replaced an
	// old fork hook with the current one).
	Installed Status = iota
	// Appended means pgbranch appended its guard line to an existing,
	// foreign hook script.
	Appended
	// AlreadyInstalled means pgbranch's hook (or guard line) was already
	// present; nothing changed.
	AlreadyInstalled
	// ManualRequired means a hook manager (husky, lefthook) owns the hooks
	// directory; pgbranch made no changes and returned instructions instead.
	ManualRequired
	// Removed means Uninstall deleted the whole hook file.
	Removed
	// LinesRemoved means Uninstall stripped only the lines it had appended,
	// leaving the rest of a foreign hook intact.
	LinesRemoved
	// NotInstalled means Uninstall found nothing to remove.
	NotInstalled
)

// Result reports what Install or Uninstall did.
type Result struct {
	Status       Status
	Path         string
	Instructions string
}

// Install adds the pgbranch post-checkout hook to repo, unless a hook
// manager (husky, lefthook) is detected, in which case it returns
// instructions instead of editing any files.
func Install(repo *gitx.Repo) (Result, error) {
	manual, instructions, err := detectManualHookManager(repo)
	if err != nil {
		return Result{}, err
	}
	if manual {
		return Result{Status: ManualRequired, Instructions: instructions}, nil
	}

	hookPath, err := postCheckoutPath(repo)
	if err != nil {
		return Result{}, err
	}

	if err := os.MkdirAll(filepath.Dir(hookPath), 0o755); err != nil {
		return Result{}, fmt.Errorf("create hooks directory: %w", err)
	}

	existing, err := os.ReadFile(hookPath)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(hookPath, []byte(Script()), 0o755); err != nil {
			return Result{}, fmt.Errorf("write hook: %w", err)
		}
		return Result{Status: Installed, Path: hookPath}, nil
	}
	if err != nil {
		return Result{}, fmt.Errorf("read existing hook: %w", err)
	}

	content := string(existing)

	if content == Script() {
		return Result{Status: AlreadyInstalled, Path: hookPath}, nil
	}

	if isOldForkHook(content) {
		if err := os.WriteFile(hookPath, []byte(Script()), 0o755); err != nil {
			return Result{}, fmt.Errorf("replace old hook: %w", err)
		}
		return Result{Status: Installed, Path: hookPath}, nil
	}

	if strings.Contains(content, GuardLine) {
		return Result{Status: AlreadyInstalled, Path: hookPath}, nil
	}

	mode := os.FileMode(0o755)
	if info, err := os.Stat(hookPath); err == nil {
		mode = info.Mode().Perm() | 0o111
	}

	newContent := content + appendedBlock()
	if err := os.WriteFile(hookPath, []byte(newContent), mode); err != nil {
		return Result{}, fmt.Errorf("append to existing hook: %w", err)
	}

	return Result{Status: Appended, Path: hookPath}, nil
}

// Uninstall removes the pgbranch post-checkout hook from repo: the whole
// file when it is entirely ours, or just the lines pgbranch appended when
// it shares the file with a foreign hook.
func Uninstall(repo *gitx.Repo) (Result, error) {
	hookPath, err := postCheckoutPath(repo)
	if err != nil {
		return Result{}, err
	}

	existing, err := os.ReadFile(hookPath)
	if errors.Is(err, os.ErrNotExist) {
		return Result{Status: NotInstalled, Path: hookPath}, nil
	}
	if err != nil {
		return Result{}, fmt.Errorf("read existing hook: %w", err)
	}

	content := string(existing)

	if content == Script() || isOldForkHook(content) {
		if err := os.Remove(hookPath); err != nil {
			return Result{}, fmt.Errorf("remove hook: %w", err)
		}
		return Result{Status: Removed, Path: hookPath}, nil
	}

	if strings.Contains(content, appendedBlock()) {
		newContent := strings.Replace(content, appendedBlock(), "", 1)
		if err := os.WriteFile(hookPath, []byte(newContent), 0o755); err != nil {
			return Result{}, fmt.Errorf("update hook: %w", err)
		}
		return Result{Status: LinesRemoved, Path: hookPath}, nil
	}

	return Result{Status: NotInstalled, Path: hookPath}, nil
}

// IsInstalled reports whether the pgbranch hook (a full hook we wrote, the
// old fork hook, or our guard line appended to a foreign hook) is present.
func IsInstalled(repo *gitx.Repo) (bool, error) {
	hookPath, err := postCheckoutPath(repo)
	if err != nil {
		return false, err
	}

	content, err := os.ReadFile(hookPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	s := string(content)
	return s == Script() || isOldForkHook(s) || strings.Contains(s, GuardLine), nil
}

func appendedBlock() string {
	return "\n# pgbranch\n" + GuardLine + "\n"
}

func isOldForkHook(content string) bool {
	return strings.Contains(content, marker) && strings.Contains(content, oldForkMarker)
}

func postCheckoutPath(repo *gitx.Repo) (string, error) {
	hooksDir, err := repo.HooksDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(hooksDir, "post-checkout"), nil
}

// detectManualHookManager reports whether husky or lefthook manage this
// repository's hooks, in which case pgbranch must not edit any files.
func detectManualHookManager(repo *gitx.Repo) (bool, string, error) {
	topLevel, err := repo.TopLevel()
	if err != nil {
		return false, "", err
	}

	if info, err := os.Stat(filepath.Join(topLevel, ".husky")); err == nil && info.IsDir() {
		return true, huskyInstructions(), nil
	}

	hooksPath, ok, err := rawCoreHooksPath(repo)
	if err != nil {
		return false, "", err
	}
	if ok && strings.Contains(hooksPath, ".husky") {
		return true, huskyInstructions(), nil
	}

	for _, name := range []string{"lefthook.yml", "lefthook.yaml", ".lefthook.yml", ".lefthook.yaml"} {
		if _, err := os.Stat(filepath.Join(topLevel, name)); err == nil {
			return true, lefthookInstructions(), nil
		}
	}

	return false, "", nil
}

// rawCoreHooksPath returns the unresolved value of core.hooksPath, if set.
func rawCoreHooksPath(repo *gitx.Repo) (string, bool, error) {
	cmd := exec.Command("git", "config", "--get", "core.hooksPath")
	cmd.Dir = repo.Dir

	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return "", false, nil
		}
		return "", false, err
	}

	return strings.TrimSpace(string(out)), true, nil
}

func huskyInstructions() string {
	return "husky manages this repository's git hooks; pgbranch did not modify any files.\n" +
		"Add the following line to your .husky/post-checkout file (creating it if needed):\n\n" +
		"  " + GuardLine + "\n"
}

func lefthookInstructions() string {
	return "lefthook manages this repository's git hooks; pgbranch did not modify any files.\n" +
		"Add a post-checkout command to your lefthook.yml, for example:\n\n" +
		"  post-checkout:\n" +
		"    commands:\n" +
		"      pgbranch:\n" +
		"        run: " + GuardLine + "\n"
}
