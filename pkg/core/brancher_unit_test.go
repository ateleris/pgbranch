package core

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/le-vlad/pgbranch/pkg/config"
	"github.com/le-vlad/pgbranch/pkg/storage"
)

// Most of the Brancher surface never touches PostgreSQL: only CreateBranch,
// Checkout, DeleteBranch and UpdateBranch do. Everything else works on files
// and in-memory metadata, so it is tested here without a container and runs
// under -short.

func TestInitializeAppliesDefaults(t *testing.T) {
	dir := t.TempDir()

	err := Initialize(dir, &config.Config{Database: "acme_dev"})
	require.NoError(t, err)

	cfg, err := config.Load(dir)
	require.NoError(t, err)

	defaults := config.DefaultConfig()
	assert.Equal(t, "acme_dev", cfg.Database)
	assert.Equal(t, defaults.Host, cfg.Host)
	assert.Equal(t, defaults.Port, cfg.Port)
	assert.Equal(t, defaults.User, cfg.User)
	assert.Equal(t, dir, cfg.Root)
}

func TestInitializeKeepsExplicitValues(t *testing.T) {
	dir := t.TempDir()

	err := Initialize(dir, &config.Config{
		Database: "acme_dev",
		Host:     "db.internal",
		Port:     5433,
		User:     "app",
		Password: "hunter2",
	})
	require.NoError(t, err)

	cfg, err := config.Load(dir)
	require.NoError(t, err)

	assert.Equal(t, "db.internal", cfg.Host)
	assert.Equal(t, 5433, cfg.Port)
	assert.Equal(t, "app", cfg.User)
	assert.Equal(t, "hunter2", cfg.Password)
}

func TestInitializeCarriesRemotesOver(t *testing.T) {
	dir := t.TempDir()

	err := Initialize(dir, &config.Config{
		Database: "acme_dev",
		Remotes: map[string]*config.RemoteConfig{
			"origin": {Name: "origin", Type: "fs", URL: "/srv/snapshots"},
		},
		DefaultRemote: "origin",
	})
	require.NoError(t, err)

	cfg, err := config.Load(dir)
	require.NoError(t, err)

	assert.Equal(t, "origin", cfg.DefaultRemote)
	require.Contains(t, cfg.Remotes, "origin")
	assert.Equal(t, "/srv/snapshots", cfg.Remotes["origin"].URL)
}

func TestInitializeWritesEmptyMetadata(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, Initialize(dir, &config.Config{Database: "acme_dev"}))

	meta, err := storage.LoadMetadata(dir)
	require.NoError(t, err)

	assert.Empty(t, meta.CurrentBranch)
	assert.Empty(t, meta.Branches)
	assert.Equal(t, dir, meta.Root)
}

func TestInitializeRejectsBadInput(t *testing.T) {
	t.Run("nil config", func(t *testing.T) {
		assert.Error(t, Initialize(t.TempDir(), nil))
	})

	t.Run("missing database", func(t *testing.T) {
		assert.Error(t, Initialize(t.TempDir(), &config.Config{}))
	})
}

func TestOpenNotInitialized(t *testing.T) {
	_, err := Open(t.TempDir())

	assert.ErrorIs(t, err, config.ErrNotInitialized)
}

func TestOpenRoundTrip(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, Initialize(dir, &config.Config{Database: "acme_dev"}))

	brancher, err := Open(dir)
	require.NoError(t, err)

	assert.Equal(t, "acme_dev", brancher.Config.Database)
	assert.Equal(t, dir, brancher.Config.Root)
	assert.Equal(t, dir, brancher.Metadata.Root)
	assert.NotNil(t, brancher.Client)
}

// Two workspaces must stay independent, which is the point of making the
// directory explicit rather than reading the process working directory.
func TestOpenIsolatesWorkspaces(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	require.NoError(t, Initialize(first, &config.Config{Database: "first_db"}))
	require.NoError(t, Initialize(second, &config.Config{Database: "second_db"}))

	a, err := Open(first)
	require.NoError(t, err)
	b, err := Open(second)
	require.NoError(t, err)

	assert.Equal(t, "first_db", a.Config.Database)
	assert.Equal(t, "second_db", b.Config.Database)

	a.Metadata.AddBranch("only-in-first", "", map[string]string{"first_db": "snap"})
	require.NoError(t, a.Metadata.Save())

	reloaded, err := Open(second)
	require.NoError(t, err)
	assert.Empty(t, reloaded.Metadata.Branches, "second workspace must be unaffected")
}

func TestNewUsesSuppliedConfigAndMetadata(t *testing.T) {
	cfg := &config.Config{Root: "/srv/acme", Database: "acme_dev"}
	meta := storage.NewMetadata("/srv/acme")

	brancher := New(cfg, meta)

	assert.Same(t, cfg, brancher.Config)
	assert.Same(t, meta, brancher.Metadata)
	require.NotNil(t, brancher.Client)
	assert.Same(t, cfg, brancher.Client.Config)
}

func newBrancherWithBranches(t *testing.T, current string, names ...string) *Brancher {
	t.Helper()

	meta := storage.NewMetadata(t.TempDir())
	for _, n := range names {
		meta.AddBranch(n, "", map[string]string{"acme_dev": n + "_snap"})
	}
	meta.CurrentBranch = current

	return New(&config.Config{Database: "acme_dev"}, meta)
}

func TestListBranchesIsSortedAndFlagsCurrent(t *testing.T) {
	b := newBrancherWithBranches(t, "main", "zeta", "main", "alpha")

	got := b.ListBranches()

	require.Len(t, got, 3)
	assert.Equal(t, []string{"alpha", "main", "zeta"},
		[]string{got[0].Name, got[1].Name, got[2].Name})

	for _, info := range got {
		assert.Equal(t, info.Name == "main", info.IsCurrent, "branch %s", info.Name)
	}
}

func TestListBranchesEmpty(t *testing.T) {
	b := newBrancherWithBranches(t, "")

	assert.Empty(t, b.ListBranches())
}

func TestCurrentBranchAndStatus(t *testing.T) {
	b := newBrancherWithBranches(t, "main", "main", "feature")

	assert.Equal(t, "main", b.CurrentBranch())

	name, count := b.Status()
	assert.Equal(t, "main", name)
	assert.Equal(t, 2, count)
}

func TestGetStaleBranchesOrdersByStaleness(t *testing.T) {
	meta := storage.NewMetadata(t.TempDir())

	fresh := meta.AddBranch("fresh", "main", map[string]string{"acme_dev": "fresh_snap"})
	fresh.CreatedAt = time.Now()

	old := meta.AddBranch("old", "main", map[string]string{"acme_dev": "old_snap"})
	old.CreatedAt = time.Now().AddDate(0, 0, -30)

	ancient := meta.AddBranch("ancient", "main", map[string]string{"acme_dev": "ancient_snap"})
	ancient.CreatedAt = time.Now().AddDate(0, 0, -90)

	b := New(&config.Config{Database: "acme_dev"}, meta)

	got := b.GetStaleBranches(DefaultStaleDays)

	require.Len(t, got, 2, "only branches past the threshold are stale")
	assert.Equal(t, "ancient", got[0].Name, "stalest first")
	assert.Equal(t, "old", got[1].Name)
}

// Branches with no parent are root branches such as main. They are never
// reported as stale, so that prune can never suggest deleting them.
func TestGetStaleBranchesSkipsRootBranches(t *testing.T) {
	meta := storage.NewMetadata(t.TempDir())

	root := meta.AddBranch("main", "", map[string]string{"acme_dev": "main_snap"})
	root.CreatedAt = time.Now().AddDate(0, 0, -365)

	child := meta.AddBranch("feature", "main", map[string]string{"acme_dev": "feature_snap"})
	child.CreatedAt = time.Now().AddDate(0, 0, -365)

	b := New(&config.Config{Database: "acme_dev"}, meta)

	got := b.GetStaleBranches(DefaultStaleDays)

	require.Len(t, got, 1)
	assert.Equal(t, "feature", got[0].Name)
}

// A recent checkout keeps a branch fresh even when it was created long ago.
func TestGetStaleBranchesUsesLastCheckout(t *testing.T) {
	meta := storage.NewMetadata(t.TempDir())

	branch := meta.AddBranch("revived", "main", map[string]string{"acme_dev": "snap"})
	branch.CreatedAt = time.Now().AddDate(0, 0, -90)
	branch.LastCheckoutAt = time.Now()

	b := New(&config.Config{Database: "acme_dev"}, meta)

	assert.Empty(t, b.GetStaleBranches(DefaultStaleDays))
}

// The current branch must never be reported as stale, even when it has a
// non-empty parent (so the "root branch" heuristic doesn't catch it) and
// hasn't been checked out again since it became current -- being actively
// worked on for a long time without switching away is not the same as
// being abandoned. Otherwise `prune -y` could delete the branch you are on.
func TestGetStaleBranchesExcludesCurrentBranch(t *testing.T) {
	meta := storage.NewMetadata(t.TempDir())

	current := meta.AddBranch("in-progress", "main", map[string]string{"acme_dev": "snap"})
	current.CreatedAt = time.Now().AddDate(0, 0, -90)
	current.LastCheckoutAt = time.Now().AddDate(0, 0, -90)
	meta.CurrentBranch = "in-progress"

	b := New(&config.Config{Database: "acme_dev"}, meta)

	assert.Empty(t, b.GetStaleBranches(DefaultStaleDays), "the current branch must never be pruned as stale")
}

// The baseline branch must never be reported as stale even if it is not a
// root branch (e.g. it was recreated from another branch after being
// deleted), matching GoneBranches' baseline exclusion.
func TestGetStaleBranchesExcludesBaselineBranch(t *testing.T) {
	meta := storage.NewMetadata(t.TempDir())

	baseline := meta.AddBranch("main", "feature", map[string]string{"acme_dev": "snap"})
	baseline.CreatedAt = time.Now().AddDate(0, 0, -90)
	baseline.LastCheckoutAt = time.Now().AddDate(0, 0, -90)

	b := New(&config.Config{Database: "acme_dev", BaselineBranch: "main"}, meta)

	assert.Empty(t, b.GetStaleBranches(DefaultStaleDays), "the baseline branch must never be pruned as stale")
}
