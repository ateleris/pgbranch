package cli

import (
	"path/filepath"

	"github.com/le-vlad/pgbranch/internal/gitx"
	"github.com/le-vlad/pgbranch/pkg/config"
)

// findWorkspace walks up from start looking for a .pgbranch directory. If
// none is found and start is inside a git repository, it falls back to the
// git top-level directory, then the main worktree root (for a linked
// worktree), in case pgbranch was initialized there instead of the current
// directory.
func findWorkspace(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}

	for {
		if config.IsInitialized(dir) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	if repo, err := gitx.Open(start); err == nil {
		if top, err := repo.TopLevel(); err == nil && config.IsInitialized(top) {
			return top, nil
		}

		if root, err := repo.MainWorktreeRoot(); err == nil && config.IsInitialized(root) {
			return root, nil
		}
	}

	// Not a git repository, or no .pgbranch directory found in it either;
	// fall back to start so callers get the usual "not initialized" error
	// naming that directory.
	return start, nil
}

// workspaceRoot returns the workspace directory a new `init` should write
// to: the git top-level directory when start is inside a git repository,
// otherwise start itself.
func workspaceRoot(start string) (string, error) {
	if repo, err := gitx.Open(start); err == nil {
		return repo.TopLevel()
	}
	return start, nil
}
