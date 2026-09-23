// Package gitx wraps the git plumbing pgbranch needs: locating hooks and
// git directories (including in linked worktrees), reading the current
// branch, listing local branches, and detecting an in-progress rebase or
// merge. All operations shell out to the git binary via os/exec.
package gitx

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Repo is a git repository (or worktree) rooted at Dir.
type Repo struct {
	Dir string
}

// Open verifies dir is inside a git repository and returns a Repo for it.
func Open(dir string) (*Repo, error) {
	r := &Repo{Dir: dir}
	if _, err := r.run("rev-parse", "--git-dir"); err != nil {
		return nil, fmt.Errorf("%s is not a git repository: %w", dir, err)
	}
	return r, nil
}

func (r *Repo) run(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}

	return strings.TrimSpace(stdout.String()), nil
}

// absolute resolves p relative to r.Dir if it is not already absolute.
func (r *Repo) absolute(p string) (string, error) {
	if filepath.IsAbs(p) {
		return filepath.Clean(p), nil
	}
	return filepath.Abs(filepath.Join(r.Dir, p))
}

// TopLevel returns the absolute path of the working tree's top-level directory.
func (r *Repo) TopLevel() (string, error) {
	out, err := r.run("rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return r.absolute(out)
}

// CommonDir returns the absolute path of the repository's common git
// directory (shared by all worktrees).
func (r *Repo) CommonDir() (string, error) {
	out, err := r.run("rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	return r.absolute(out)
}

// GitDir returns the absolute path of this worktree's git directory. For a
// linked worktree this differs from CommonDir.
func (r *Repo) GitDir() (string, error) {
	out, err := r.run("rev-parse", "--git-dir")
	if err != nil {
		return "", err
	}
	return r.absolute(out)
}

// MainWorktreeRoot returns the working directory of the main (non-bare)
// worktree, i.e. the parent of the common git directory.
func (r *Repo) MainWorktreeRoot() (string, error) {
	commonDir, err := r.CommonDir()
	if err != nil {
		return "", err
	}
	return filepath.Dir(commonDir), nil
}

// IsLinkedWorktree reports whether Dir is a linked worktree (as opposed to
// the main worktree).
func (r *Repo) IsLinkedWorktree() (bool, error) {
	gitDir, err := r.GitDir()
	if err != nil {
		return false, err
	}
	commonDir, err := r.CommonDir()
	if err != nil {
		return false, err
	}
	return gitDir != commonDir, nil
}

// HooksDir returns the directory git looks in for hooks: core.hooksPath if
// set (resolved relative to the top-level working directory when it is a
// relative path), otherwise <common-git-dir>/hooks.
func (r *Repo) HooksDir() (string, error) {
	out, err := r.run("config", "--get", "core.hooksPath")
	if err == nil && strings.TrimSpace(out) != "" {
		hooksPath := strings.TrimSpace(out)
		if filepath.IsAbs(hooksPath) {
			return filepath.Clean(hooksPath), nil
		}
		topLevel, err := r.TopLevel()
		if err != nil {
			return "", err
		}
		return filepath.Join(topLevel, hooksPath), nil
	}

	commonDir, err := r.CommonDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(commonDir, "hooks"), nil
}

// CurrentBranch returns the short name of the currently checked out branch,
// or "" when HEAD is detached. It uses `git symbolic-ref` rather than
// `git rev-parse --abbrev-ref HEAD`, which disambiguates a branch from a
// same-named tag by returning "heads/<branch>" instead of the plain name.
func (r *Repo) CurrentBranch() (string, error) {
	out, err := r.run("symbolic-ref", "-q", "HEAD")
	if err == nil {
		return strings.TrimPrefix(out, "refs/heads/"), nil
	}
	// A non-zero exit here means HEAD is detached (or does not point
	// to a branch), not a real error.
	return "", nil
}

// LocalBranches lists all local branch names.
func (r *Repo) LocalBranches() ([]string, error) {
	// refname:lstrip=2 strips "refs/heads/" unconditionally, unlike
	// refname:short, which disambiguates a branch from a same-named tag by
	// printing "heads/<branch>" instead of the plain branch name.
	out, err := r.run("for-each-ref", "--format=%(refname:lstrip=2)", "refs/heads")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// BranchExists reports whether a local branch with the given name exists.
func (r *Repo) BranchExists(name string) (bool, error) {
	branches, err := r.LocalBranches()
	if err != nil {
		return false, err
	}
	for _, b := range branches {
		if b == name {
			return true, nil
		}
	}
	return false, nil
}

// OperationInProgress reports whether a rebase, merge, cherry-pick, or
// bisect is currently in progress in this worktree.
func (r *Repo) OperationInProgress() (bool, error) {
	gitDir, err := r.GitDir()
	if err != nil {
		return false, err
	}

	markers := []string{"rebase-merge", "rebase-apply", "MERGE_HEAD", "CHERRY_PICK_HEAD", "BISECT_LOG"}
	for _, m := range markers {
		if _, err := os.Stat(filepath.Join(gitDir, m)); err == nil {
			return true, nil
		}
	}
	return false, nil
}
