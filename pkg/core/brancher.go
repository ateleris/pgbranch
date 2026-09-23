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

// BranchError reports a failed operation on a named branch. Test the cause
// with errors.Is against the sentinels above, or recover the branch name with
// errors.As:
//
//	var be *core.BranchError
//	if errors.As(err, &be) { log.Println(be.Name) }
//
// The message deliberately carries no guidance about command line flags, so
// that a caller embedding pgbranch is not told to "use --force".
type BranchError struct {
	// Name is the branch the operation was attempted on.
	Name string
	// Err is the sentinel describing what went wrong.
	Err error

	msg string
}

func (e *BranchError) Error() string { return e.msg }

func (e *BranchError) Unwrap() error { return e.Err }

func branchNotFound(name string) error {
	return &BranchError{
		Name: name,
		Err:  ErrBranchNotFound,
		msg:  fmt.Sprintf("branch '%s' does not exist", name),
	}
}

func branchExists(name string) error {
	return &BranchError{
		Name: name,
		Err:  ErrBranchExists,
		msg:  fmt.Sprintf("branch '%s' already exists", name),
	}
}

func currentBranchError(name string) error {
	return &BranchError{
		Name: name,
		Err:  ErrCurrentBranch,
		msg:  fmt.Sprintf("cannot delete the current branch '%s'", name),
	}
}

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
// meta.Root, so both must be set for Save to succeed. cfg is normalized
// (migrating a legacy single-database config) and meta's legacy snapshot
// field is migrated to the per-database Snapshots map.
func New(cfg *config.Config, meta *storage.Metadata) *Brancher {
	cfg.Normalize()
	meta.MigrateSnapshots(cfg.PrimaryDatabase())
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
	stored.Databases = cfg.Databases
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
	if cfg.BaselineBranch != "" {
		stored.BaselineBranch = cfg.BaselineBranch
	}
	if cfg.NewBranchFrom != "" {
		stored.NewBranchFrom = cfg.NewBranchFrom
	}
	stored.FollowWorktrees = cfg.FollowWorktrees
	stored.Remotes = cfg.Remotes
	stored.DefaultRemote = cfg.DefaultRemote

	stored.Normalize()

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

// parseStrategy parses a database's configured clone strategy string.
func parseStrategy(s string) (postgres.Strategy, error) {
	return postgres.ParseStrategy(s)
}

// CreateBranch creates a new branch. If from is empty, the branch is created
// from the current working databases. Otherwise it is cloned from the
// named branch's snapshots (saving that branch first if it is the current
// branch, so the clone reflects its latest state).
func (b *Brancher) CreateBranch(ctx context.Context, name, from string) error {
	if b.Metadata.BranchExists(name) {
		return branchExists(name)
	}

	var sources map[string]string
	if from != "" {
		if from == b.Metadata.CurrentBranch {
			if err := b.UpdateBranch(ctx, from); err != nil {
				return fmt.Errorf("failed to save branch '%s': %w", from, err)
			}
		}
		fromBranch, ok := b.Metadata.GetBranch(from)
		if !ok {
			return branchNotFound(from)
		}
		sources = fromBranch.Snapshots
	}

	snapshots := make(map[string]string, len(b.Config.Databases))
	var created []string

	rollback := func() {
		for _, snap := range created {
			_ = b.Client.DropDatabaseByName(ctx, snap)
		}
	}

	for _, db := range b.Config.Databases {
		src := db.Name
		if sources != nil {
			s, ok := sources[db.Name]
			if !ok || s == "" {
				rollback()
				return fmt.Errorf("branch '%s' has no snapshot for database '%s'", from, db.Name)
			}
			src = s
		}

		strategy, err := parseStrategy(db.Strategy)
		if err != nil {
			rollback()
			return err
		}

		snap := storage.SnapshotDBName(db.Name, name)
		if err := b.Client.CloneDatabase(ctx, src, snap, strategy); err != nil {
			rollback()
			return fmt.Errorf("failed to create snapshot for database '%s': %w", db.Name, err)
		}

		created = append(created, snap)
		snapshots[db.Name] = snap
	}

	parent := b.Metadata.CurrentBranch
	branch := b.Metadata.AddBranch(name, parent, snapshots)
	branch.Snapshot = snapshots[b.Config.PrimaryDatabase()]

	if err := b.Metadata.Save(); err != nil {
		rollback()
		return fmt.Errorf("failed to save metadata: %w", err)
	}

	return nil
}

// Checkout switches to the specified branch by replacing each working
// database with a copy of the branch's snapshot for it. The current
// branch's state is saved first.
func (b *Brancher) Checkout(ctx context.Context, name string) error {
	branch, ok := b.Metadata.GetBranch(name)
	if !ok {
		return branchNotFound(name)
	}

	if b.Metadata.CurrentBranch != "" && b.Metadata.CurrentBranch != name {
		if err := b.UpdateBranch(ctx, b.Metadata.CurrentBranch); err != nil {
			return fmt.Errorf("failed to save current branch '%s': %w", b.Metadata.CurrentBranch, err)
		}
	}

	for _, db := range b.Config.Databases {
		snap, ok := branch.Snapshots[db.Name]
		if !ok || snap == "" {
			return fmt.Errorf("branch '%s' has no snapshot for database '%s'", name, db.Name)
		}

		strategy, err := parseStrategy(db.Strategy)
		if err != nil {
			return err
		}

		if err := b.Client.ReplaceDatabase(ctx, snap, db.Name, strategy); err != nil {
			return fmt.Errorf("failed to restore database '%s': %w", db.Name, err)
		}
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

// DeleteBranch removes a branch and its associated snapshot databases.
// Returns an error if trying to delete the current branch without force. If
// dropping some snapshot databases fails, the others are still attempted,
// the branch is still removed from metadata, and the errors are joined and
// returned.
func (b *Brancher) DeleteBranch(ctx context.Context, name string, force bool) error {
	if name == b.Metadata.CurrentBranch && !force {
		return currentBranchError(name)
	}

	branch, ok := b.Metadata.GetBranch(name)
	if !ok {
		return branchNotFound(name)
	}

	var errs []error
	for db, snap := range branch.Snapshots {
		if snap == "" {
			continue
		}
		if err := b.Client.DropDatabaseByName(ctx, snap); err != nil {
			errs = append(errs, fmt.Errorf("database '%s': %w", db, err))
		}
	}

	if err := b.Metadata.DeleteBranch(name); err != nil {
		errs = append(errs, err)
	}

	if b.Metadata.CurrentBranch == name {
		b.Metadata.CurrentBranch = ""
	}

	if err := b.Metadata.Save(); err != nil {
		errs = append(errs, fmt.Errorf("failed to save metadata: %w", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to delete branch '%s': %w", name, errors.Join(errs...))
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

// UpdateBranch updates an existing branch's snapshots to match the current
// state of the working databases.
func (b *Brancher) UpdateBranch(ctx context.Context, name string) error {
	branch, ok := b.Metadata.GetBranch(name)
	if !ok {
		return branchNotFound(name)
	}
	if branch.Snapshots == nil {
		branch.Snapshots = make(map[string]string, len(b.Config.Databases))
	}

	for _, db := range b.Config.Databases {
		snap, ok := branch.Snapshots[db.Name]
		if !ok || snap == "" {
			snap = storage.SnapshotDBName(db.Name, name)
			branch.Snapshots[db.Name] = snap
		}

		strategy, err := parseStrategy(db.Strategy)
		if err != nil {
			return err
		}

		if err := b.Client.ReplaceDatabase(ctx, db.Name, snap, strategy); err != nil {
			return fmt.Errorf("failed to update database '%s' for branch '%s': %w", db.Name, name, err)
		}
	}

	branch.Snapshot = branch.Snapshots[b.Config.PrimaryDatabase()]

	return b.Metadata.Save()
}

// Reset recreates the working databases from the given branch's snapshots
// (from the baseline branch if from is empty), discarding all current
// state, then overwrites the current branch's snapshots with the result.
func (b *Brancher) Reset(ctx context.Context, from string) error {
	if from == "" {
		from = b.Config.BaselineBranchOrDefault()
	}

	branch, ok := b.Metadata.GetBranch(from)
	if !ok {
		return branchNotFound(from)
	}

	for _, db := range b.Config.Databases {
		snap, ok := branch.Snapshots[db.Name]
		if !ok || snap == "" {
			return fmt.Errorf("branch '%s' has no snapshot for database '%s'", from, db.Name)
		}

		strategy, err := parseStrategy(db.Strategy)
		if err != nil {
			return err
		}

		if err := b.Client.ReplaceDatabase(ctx, snap, db.Name, strategy); err != nil {
			return fmt.Errorf("failed to reset database '%s': %w", db.Name, err)
		}
	}

	if current := b.Metadata.CurrentBranch; current != "" {
		if err := b.UpdateBranch(ctx, current); err != nil {
			return err
		}
	}

	return nil
}

// Sync checks out the DB branch matching gitBranch, creating it first if it
// doesn't exist yet. A newly created branch is cloned per NewBranchFrom:
// from the baseline branch if it exists as a DB branch (config
// new_branch_from "baseline", the default), otherwise from the current
// working state.
func (b *Brancher) Sync(ctx context.Context, gitBranch string) error {
	if b.Metadata.BranchExists(gitBranch) {
		return b.Checkout(ctx, gitBranch)
	}

	from := ""
	if b.Config.NewBranchFromOrDefault() == "baseline" {
		baseline := b.Config.BaselineBranchOrDefault()
		if b.Metadata.BranchExists(baseline) {
			from = baseline
		}
	}

	if err := b.CreateBranch(ctx, gitBranch, from); err != nil {
		return err
	}

	return b.Checkout(ctx, gitBranch)
}

// GoneBranches returns the DB branches whose git branch no longer exists
// locally, excluding the baseline branch and the current branch.
func (b *Brancher) GoneBranches(localGitBranches []string) []string {
	existing := make(map[string]bool, len(localGitBranches))
	for _, n := range localGitBranches {
		existing[n] = true
	}

	baseline := b.Config.BaselineBranchOrDefault()

	var gone []string
	for _, info := range b.ListBranches() {
		if info.Name == baseline || info.Name == b.Metadata.CurrentBranch {
			continue
		}
		if !existing[info.Name] {
			gone = append(gone, info.Name)
		}
	}

	sort.Strings(gone)
	return gone
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
func (b *Brancher) PruneBranches(ctx context.Context, names []string) (deleted []string, errs []error) {
	for _, name := range names {
		if err := b.DeleteBranch(ctx, name, true); err != nil {
			errs = append(errs, fmt.Errorf("failed to delete '%s': %w", name, err))
		} else {
			deleted = append(deleted, name)
		}
	}
	return deleted, errs
}
