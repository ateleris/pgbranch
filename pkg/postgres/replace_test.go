package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/le-vlad/pgbranch/internal/testutil"
)

func TestReplaceDatabase_HappyPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()

	pg, err := testutil.StartPostgresContainer(ctx)
	require.NoError(t, err)
	defer func() { _ = pg.Stop(ctx) }()

	cfg := pg.GetConfig()
	client := NewClient(cfg)

	snapshot := "replace_snapshot"
	mustExecSQL(t, ctx, cfg, cfg.Database, `
		CREATE TABLE users (id SERIAL PRIMARY KEY, name TEXT);
		INSERT INTO users (name) VALUES ('alice'), ('bob');
	`)
	require.NoError(t, client.CloneDatabase(ctx, cfg.Database, snapshot, StrategyTemplate))
	defer func() { _ = client.DropDatabaseByName(ctx, snapshot) }()

	// Diverge the working database from the snapshot.
	mustExecSQL(t, ctx, cfg, cfg.Database, `INSERT INTO users (name) VALUES ('carol');`)
	assert.Equal(t, 3, mustCountRows(t, ctx, cfg, cfg.Database, "users"))

	err = client.ReplaceDatabase(ctx, snapshot, cfg.Database, StrategyTemplate)
	require.NoError(t, err)

	assert.Equal(t, 2, mustCountRows(t, ctx, cfg, cfg.Database, "users"))

	tmpExists, err := client.Exists(ctx, scratchDBName(cfg.Database))
	require.NoError(t, err)
	assert.False(t, tmpExists, "scratch database should not remain after a successful replace")
}

func TestReplaceDatabase_CloneFailureLeavesTargetIntact(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()

	pg, err := testutil.StartPostgresContainer(ctx)
	require.NoError(t, err)
	defer func() { _ = pg.Stop(ctx) }()

	cfg := pg.GetConfig()
	client := NewClient(cfg)

	mustExecSQL(t, ctx, cfg, cfg.Database, `
		CREATE TABLE orders (id SERIAL PRIMARY KEY);
		INSERT INTO orders DEFAULT VALUES;
	`)

	missingSrc := "does_not_exist_source_db"
	err = client.ReplaceDatabase(ctx, missingSrc, cfg.Database, StrategyTemplate)
	require.Error(t, err)

	// target must be untouched.
	assert.Equal(t, 1, mustCountRows(t, ctx, cfg, cfg.Database, "orders"))

	tmpExists, err := client.Exists(ctx, scratchDBName(cfg.Database))
	require.NoError(t, err)
	assert.False(t, tmpExists, "scratch database must be cleaned up after a failed clone")
}

func TestScratchDBName_FitsWithinIdentifierLimit(t *testing.T) {
	target := "db_pgbranch_" + strings.Repeat("a", 51) // exactly 63 bytes
	require.Len(t, target, 63)

	scratch := scratchDBName(target)
	assert.LessOrEqual(t, len(scratch), maxIdentifierLen)
	assert.NotEqual(t, target, scratch, "scratch name must never collide with a 63-byte target")
}

func TestReplaceDatabase_RejectsOverlongIdentifiers(t *testing.T) {
	ctx := context.Background()
	client := &Client{}

	tooLong := strings.Repeat("a", 64)

	err := client.ReplaceDatabase(ctx, "src", tooLong, StrategyTemplate)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeding the Postgres limit")
}

func TestCloneDatabase_RejectsOverlongIdentifiers(t *testing.T) {
	ctx := context.Background()
	client := &Client{}

	tooLong := strings.Repeat("a", 64)

	err := client.CloneDatabase(ctx, tooLong, "dst", StrategyTemplate)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeding the Postgres limit")

	err = client.CloneDatabase(ctx, "src", tooLong, StrategyTemplate)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeding the Postgres limit")
}

// TestReplaceDatabase_63ByteSnapshotName is an integration test reproducing
// the scenario from storage.SnapshotDBName: a snapshot name that is exactly
// 63 bytes (the Postgres identifier limit). Replacing such a target used to
// collide with the scratch database name (target+"__pgbranch_tmp" is
// truncated by Postgres back to exactly target for a 63-byte target),
// destroying the snapshot instead of updating it.
func TestReplaceDatabase_63ByteSnapshotName(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()

	pg, err := testutil.StartPostgresContainer(ctx)
	require.NoError(t, err)
	defer func() { _ = pg.Stop(ctx) }()

	cfg := pg.GetConfig()
	client := NewClient(cfg)

	snapshot := "db_pgbranch_" + strings.Repeat("a", 51)
	require.Len(t, snapshot, 63)

	mustExecSQL(t, ctx, cfg, cfg.Database, `
		CREATE TABLE users (id SERIAL PRIMARY KEY, name TEXT);
		INSERT INTO users (name) VALUES ('alice'), ('bob');
	`)
	require.NoError(t, client.CloneDatabase(ctx, cfg.Database, snapshot, StrategyTemplate))
	defer func() { _ = client.DropDatabaseByName(ctx, snapshot) }()

	// Diverge the working database, then update the (63-byte) snapshot from it,
	// as UpdateBranch does on checkout/save.
	mustExecSQL(t, ctx, cfg, cfg.Database, `INSERT INTO users (name) VALUES ('carol');`)
	err = client.ReplaceDatabase(ctx, cfg.Database, snapshot, StrategyTemplate)
	require.NoError(t, err)

	exists, err := client.Exists(ctx, snapshot)
	require.NoError(t, err)
	require.True(t, exists, "63-byte snapshot must still exist after replace")

	assert.Equal(t, 3, mustCountRows(t, ctx, cfg, snapshot, "users"))
}
