// Package core provides the main business logic for pgbranch,
// implementing database branching operations using PostgreSQL template databases.
package core

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/le-vlad/pgbranch/pkg/config"
	"github.com/le-vlad/pgbranch/pkg/postgres"
	"github.com/le-vlad/pgbranch/pkg/storage"
)

// Errors returned by Brancher operations. Callers should test for them with
// errors.Is rather than matching on message text.
var (
	// ErrBranchNotFound is returned when the named branch does not exist.
	ErrBranchNotFound = errors.New("branch does not exist")
	// ErrBranchExists is returned when creating a branch whose name is taken.
	ErrBranchExists = errors.New("branch already exists")
	// ErrCurrentBranch is returned when deleting the checked out branch
	// without forcing.
	ErrCurrentBranch = errors.New("cannot delete the current branch")
)

// Brancher manages database branches, coordinating between the PostgreSQL
// client, configuration, and metadata storage.
type Brancher struct {
	Config   *config.Config
	Metadata *storage.Metadata
	Client   *postgres.Client
}

// Open creates a Brancher by loading the configuration and metadata from the
// given workspace directory. Returns an error wrapping config.ErrNotInitialized
// if pgbranch has not been initialized there.
func Open(dir string) (*Brancher, error) {
	if !config.IsInitialized(dir) {
		return nil, fmt.Errorf("%w in %s", config.ErrNotInitialized, dir)
	}

	cfg, err := config.Load(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	meta, err := storage.LoadMetadata(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to load metadata: %w", err)
	}

	return New(cfg, meta), nil
}

// New creates a Brancher from an in-memory configuration and metadata set,
// without touching the filesystem. Persisting operations use cfg.Root and
// meta.Root, so both must be set for Save to succeed.
func New(cfg *config.Config, meta *storage.Metadata) *Brancher {
	return &Brancher{
		Config:   cfg,
		Metadata: meta,
		Client:   postgres.NewClient(cfg),
	}
}

// Initialize sets up pgbranch in the given workspace directory using cfg for
// the database connection settings. Fields left empty on cfg fall back to the
// values from config.DefaultConfig.
func Initialize(dir string, cfg *config.Config) error {
	if cfg == nil {
		return fmt.Errorf("config is required")
	}

	if err := config.EnsureDir(config.RootDir(dir)); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	stored := config.DefaultConfig()
	stored.Root = dir
	stored.Database = cfg.Database
	if cfg.Host != "" {
		stored.Host = cfg.Host
	}
	if cfg.Port != 0 {
		stored.Port = cfg.Port
	}
	if cfg.User != "" {
		stored.User = cfg.User
	}
	stored.Password = cfg.Password
	stored.Remotes = cfg.Remotes
	stored.DefaultRemote = cfg.DefaultRemote

	if err := stored.Validate(); err != nil {
		return err
	}

	if err := stored.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	if err := storage.NewMetadata(dir).Save(); err != nil {
		return fmt.Errorf("failed to save metadata: %w", err)
	}

	return nil
}

// CreateBranch creates a new branch from the current database state.
// The branch is stored as a PostgreSQL template database.
func (b *Brancher) CreateBranch(ctx context.Context, name string) error {
	if b.Metadata.BranchExists(name) {
		return fmt.Errorf("%w: '%s'", ErrBranchExists, name)
	}

	snapshotDBName := storage.SnapshotDBName(b.Config.Database, name)

	if err := b.Client.CreateSnapshot(ctx, snapshotDBName); err != nil {
		return fmt.Errorf("failed to create snapshot: %w", err)
	}

	parent := b.Metadata.CurrentBranch
	b.Metadata.AddBranch(name, parent, snapshotDBName)

	if err := b.Metadata.Save(); err != nil {
		b.Client.DeleteSnapshot(ctx, snapshotDBName)
		return fmt.Errorf("failed to save metadata: %w", err)
	}

	return nil
}

// Checkout switches to the specified branch by replacing the working database
// with a copy of the branch's snapshot. The current branch state is saved
// before switching.
func (b *Brancher) Checkout(ctx context.Context, name string) error {
	branch, ok := b.Metadata.GetBranch(name)
	if !ok {
		return fmt.Errorf("%w: '%s'", ErrBranchNotFound, name)
	}

	if b.Metadata.CurrentBranch != "" && b.Metadata.CurrentBranch != name {
		if err := b.UpdateBranch(ctx, b.Metadata.CurrentBranch); err != nil {
			return fmt.Errorf("failed to save current branch '%s': %w", b.Metadata.CurrentBranch, err)
		}
	}

	snapshotDBName := branch.Snapshot

	if err := b.Client.RestoreFromSnapshot(ctx, snapshotDBName); err != nil {
		return fmt.Errorf("failed to restore branch: %w", err)
	}

	b.Metadata.CurrentBranch = name

	if err := b.Metadata.UpdateLastCheckout(name); err != nil {
		return fmt.Errorf("failed to update last checkout time: %w", err)
	}

	if err := b.Metadata.Save(); err != nil {
		return fmt.Errorf("failed to update metadata: %w", err)
	}

	return nil
}

// DeleteBranch removes a branch and its associated snapshot database.
// Returns an error if trying to delete the current branch without force.
func (b *Brancher) DeleteBranch(ctx context.Context, name string, force bool) error {
	if name == b.Metadata.CurrentBranch && !force {
		return fmt.Errorf("%w '%s': force the deletion to override", ErrCurrentBranch, name)
	}

	branch, ok := b.Metadata.GetBranch(name)
	if !ok {
		return fmt.Errorf("%w: '%s'", ErrBranchNotFound, name)
	}

	if err := b.Client.DeleteSnapshot(ctx, branch.Snapshot); err != nil {
		return fmt.Errorf("failed to delete snapshot database: %w", err)
	}

	if err := b.Metadata.DeleteBranch(name); err != nil {
		return err
	}

	if b.Metadata.CurrentBranch == name {
		b.Metadata.CurrentBranch = ""
	}

	if err := b.Metadata.Save(); err != nil {
		return fmt.Errorf("failed to save metadata: %w", err)
	}

	return nil
}

// BranchInfo contains information about a branch for display purposes.
type BranchInfo struct {
	Name      string
	IsCurrent bool
	Branch    *storage.Branch
}

// ListBranches returns all branches sorted alphabetically by name.
func (b *Brancher) ListBranches() []BranchInfo {
	branches := make([]BranchInfo, 0, len(b.Metadata.Branches))

	for name, branch := range b.Metadata.Branches {
		branches = append(branches, BranchInfo{
			Name:      name,
			IsCurrent: name == b.Metadata.CurrentBranch,
			Branch:    branch,
		})
	}

	sort.Slice(branches, func(i, j int) bool {
		return branches[i].Name < branches[j].Name
	})

	return branches
}

// CurrentBranch returns the name of the currently checked out branch.
func (b *Brancher) CurrentBranch() string {
	return b.Metadata.CurrentBranch
}

// Status returns the current branch name and total number of branches.
func (b *Brancher) Status() (currentBranch string, branchCount int) {
	return b.Metadata.CurrentBranch, len(b.Metadata.Branches)
}

// UpdateBranch updates an existing branch's snapshot to match the current
// database state.
func (b *Brancher) UpdateBranch(ctx context.Context, name string) error {
	branch, ok := b.Metadata.GetBranch(name)
	if !ok {
		return fmt.Errorf("%w: '%s'", ErrBranchNotFound, name)
	}

	snapshotDBName := branch.Snapshot

	if err := b.Client.DeleteSnapshot(ctx, snapshotDBName); err != nil {
		return fmt.Errorf("failed to delete old snapshot: %w", err)
	}

	if err := b.Client.CreateSnapshot(ctx, snapshotDBName); err != nil {
		return fmt.Errorf("failed to create updated snapshot: %w", err)
	}

	return nil
}

// DefaultStaleDays is the default number of days after which a branch
// is considered stale.
const DefaultStaleDays = 7

// GetStaleBranches returns branches that haven't been accessed in the
// specified number of days, sorted by staleness (oldest first).
func (b *Brancher) GetStaleBranches(staleDays int) []BranchInfo {
	staleBranches := b.Metadata.GetStaleBranches(staleDays)
	result := make([]BranchInfo, 0, len(staleBranches))

	for _, branch := range staleBranches {
		result = append(result, BranchInfo{
			Name:      branch.Name,
			IsCurrent: branch.Name == b.Metadata.CurrentBranch,
			Branch:    branch,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Branch.DaysSinceLastAccess() > result[j].Branch.DaysSinceLastAccess()
	})

	return result
}

// PruneBranches deletes multiple branches by name, returning the list of
// successfully deleted branches and any errors encountered.
func (b *Brancher) PruneBranches(ctx context.Context, names []string) (deleted []string, errors []error) {
	for _, name := range names {
		if err := b.DeleteBranch(ctx, name, true); err != nil {
			errors = append(errors, fmt.Errorf("failed to delete '%s': %w", name, err))
		} else {
			deleted = append(deleted, name)
		}
	}
	return deleted, errors
}
