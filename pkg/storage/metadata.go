// Package storage provides persistent storage for branch metadata and snapshots.
package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/le-vlad/pgbranch/pkg/config"
)

// MetadataFileName is the name of the metadata file in the pgbranch directory.
const MetadataFileName = "metadata.json"

// Branch represents a database branch with its metadata.
type Branch struct {
	Name           string    `json:"name"`
	CreatedAt      time.Time `json:"created_at"`
	LastCheckoutAt time.Time `json:"last_checkout_at,omitempty"`
	Parent         string    `json:"parent,omitempty"`

	// Snapshots maps each working database name to its snapshot database
	// name for this branch.
	Snapshots map[string]string `json:"snapshots,omitempty"`

	// Snapshot is the snapshot database name for the primary (first
	// configured) database. Deprecated: use Snapshots. Old metadata files
	// that only have this field are migrated in memory by
	// Metadata.MigrateSnapshots. Kept populated for the primary database so
	// library callers that read it directly do not break.
	Snapshot string `json:"snapshot,omitempty"`
}

// SnapshotFor returns the snapshot database name for the given working
// database, or "" if the branch has no snapshot for it.
func (b *Branch) SnapshotFor(db string) string {
	return b.Snapshots[db]
}

// IsStale returns true if the branch hasn't been accessed in the specified
// number of days.
func (b *Branch) IsStale(staleDays int) bool {
	threshold := time.Now().AddDate(0, 0, -staleDays)

	// If never checked out, use CreatedAt
	if b.LastCheckoutAt.IsZero() {
		return b.CreatedAt.Before(threshold)
	}

	return b.LastCheckoutAt.Before(threshold)
}

// DaysSinceLastAccess returns the number of days since the branch was last
// accessed (checked out or created).
func (b *Branch) DaysSinceLastAccess() int {
	var lastAccess time.Time
	if b.LastCheckoutAt.IsZero() {
		lastAccess = b.CreatedAt
	} else {
		lastAccess = b.LastCheckoutAt
	}

	return int(time.Since(lastAccess).Hours() / 24)
}

// Metadata stores information about all branches and the current branch state.
type Metadata struct {
	// Root is the workspace directory that holds the .pgbranch directory.
	// It is set by LoadMetadata and used by Save; it is never serialized.
	Root string `json:"-"`

	CurrentBranch string             `json:"current_branch"`
	Branches      map[string]*Branch `json:"branches"`
}

// NewMetadata creates a new empty Metadata instance rooted at the given
// workspace directory.
func NewMetadata(dir string) *Metadata {
	return &Metadata{
		Root:          dir,
		CurrentBranch: "",
		Branches:      make(map[string]*Branch),
	}
}

// MetadataPath returns the path to the metadata file inside the given
// workspace directory.
func MetadataPath(dir string) string {
	return filepath.Join(config.RootDir(dir), MetadataFileName)
}

// LoadMetadata reads and parses the metadata file from the given workspace
// directory. If the file doesn't exist, returns a new empty Metadata instance.
func LoadMetadata(dir string) (*Metadata, error) {
	data, err := os.ReadFile(MetadataPath(dir))
	if err != nil {
		if os.IsNotExist(err) {
			return NewMetadata(dir), nil
		}
		return nil, fmt.Errorf("failed to read metadata file: %w", err)
	}

	var meta Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("failed to parse metadata file: %w", err)
	}
	meta.Root = dir

	if meta.Branches == nil {
		meta.Branches = make(map[string]*Branch)
	}

	return &meta, nil
}

// Save writes the metadata to the metadata file under Root.
func (m *Metadata) Save() error {
	if m.Root == "" {
		return fmt.Errorf("metadata Root is not set")
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize metadata: %w", err)
	}

	if err := os.WriteFile(MetadataPath(m.Root), data, 0644); err != nil {
		return fmt.Errorf("failed to write metadata file: %w", err)
	}

	return nil
}

// AddBranch creates and adds a new branch to the metadata. snapshots maps
// each working database name to its snapshot database name for this branch.
func (m *Metadata) AddBranch(name, parent string, snapshots map[string]string) *Branch {
	branch := &Branch{
		Name:      name,
		CreatedAt: time.Now(),
		Parent:    parent,
		Snapshots: snapshots,
	}
	m.Branches[name] = branch
	return branch
}

// MigrateSnapshots migrates branches that only have the legacy Snapshot
// field set (from an older metadata file) to the Snapshots map, keyed by
// primary. It is a no-op for branches that already have Snapshots set. It
// also keeps Snapshot in sync with Snapshots[primary] going the other way,
// for branches created after the migration to multiple databases whose
// Snapshots map does not (yet) have an entry for primary.
func (m *Metadata) MigrateSnapshots(primary string) {
	if primary == "" {
		return
	}
	for _, b := range m.Branches {
		if len(b.Snapshots) == 0 && b.Snapshot != "" {
			b.Snapshots = map[string]string{primary: b.Snapshot}
			continue
		}
		if b.Snapshot == "" {
			if snap, ok := b.Snapshots[primary]; ok {
				b.Snapshot = snap
			}
		}
	}
}

// GetBranch returns the branch with the given name, or false if not found.
func (m *Metadata) GetBranch(name string) (*Branch, bool) {
	branch, ok := m.Branches[name]
	return branch, ok
}

// DeleteBranch removes a branch from the metadata.
func (m *Metadata) DeleteBranch(name string) error {
	if _, ok := m.Branches[name]; !ok {
		return fmt.Errorf("branch '%s' does not exist", name)
	}
	delete(m.Branches, name)
	return nil
}

// BranchExists returns true if a branch with the given name exists.
func (m *Metadata) BranchExists(name string) bool {
	_, ok := m.Branches[name]
	return ok
}

// ListBranches returns a list of all branch names.
func (m *Metadata) ListBranches() []string {
	names := make([]string, 0, len(m.Branches))
	for name := range m.Branches {
		names = append(names, name)
	}
	return names
}

// SetCurrentBranch sets the current branch to the given name.
func (m *Metadata) SetCurrentBranch(name string) error {
	if name != "" && !m.BranchExists(name) {
		return fmt.Errorf("branch '%s' does not exist", name)
	}
	m.CurrentBranch = name
	return nil
}

// GetStaleBranches returns all branches that haven't been accessed
// in the specified number of days. Excludes root branches (branches with no parent)
// as they represent the main base branch of the project.
func (m *Metadata) GetStaleBranches(staleDays int) []*Branch {
	var stale []*Branch
	for _, branch := range m.Branches {
		// We don't want to have our main branches stale and get them removed by accident.
		// Even though it is unlikely that they will be stale, we still want to exclude them.
		if branch.Parent == "" {
			continue
		}
		if branch.IsStale(staleDays) {
			stale = append(stale, branch)
		}
	}
	return stale
}

// UpdateLastCheckout updates the last checkout time for the given branch.
func (m *Metadata) UpdateLastCheckout(name string) error {
	branch, ok := m.Branches[name]
	if !ok {
		return fmt.Errorf("branch '%s' does not exist", name)
	}
	branch.LastCheckoutAt = time.Now()
	return nil
}
