package postgres

import (
	"context"
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

	tmpExists, err := client.Exists(ctx, cfg.Database+tmpSuffix)
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

	tmpExists, err := client.Exists(ctx, cfg.Database+tmpSuffix)
	require.NoError(t, err)
	assert.False(t, tmpExists, "scratch database must be cleaned up after a failed clone")
}
