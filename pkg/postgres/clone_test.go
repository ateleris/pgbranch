package postgres

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/le-vlad/pgbranch/internal/testutil"
)

func TestCloneDatabase_Template(t *testing.T) {
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
		CREATE TABLE items (id SERIAL PRIMARY KEY, name TEXT);
		INSERT INTO items (name) VALUES ('a'), ('b'), ('c');
	`)

	dst := "clone_template_dst"
	err = client.CloneDatabase(ctx, cfg.Database, dst, StrategyTemplate)
	require.NoError(t, err)
	defer func() { _ = client.DropDatabaseByName(ctx, dst) }()

	exists, err := client.Exists(ctx, dst)
	require.NoError(t, err)
	assert.True(t, exists)

	assert.Equal(t, 3, mustCountRows(t, ctx, cfg, dst, "items"))
}

func TestCloneDatabase_Template_WhileConnectionOpen(t *testing.T) {
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
		CREATE TABLE items (id SERIAL PRIMARY KEY, name TEXT);
		INSERT INTO items (name) VALUES ('a');
	`)

	// Hold an open connection to the source database; CloneDatabase must
	// still succeed by terminating it (and retrying if needed).
	conn, err := pgx.Connect(ctx, cfg.ConnectionURLForDB(cfg.Database))
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }()

	dst := "clone_template_busy_dst"
	err = client.CloneDatabase(ctx, cfg.Database, dst, StrategyTemplate)
	require.NoError(t, err)
	defer func() { _ = client.DropDatabaseByName(ctx, dst) }()

	exists, err := client.Exists(ctx, dst)
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestCloneDatabase_DestinationAlreadyExists(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()

	pg, err := testutil.StartPostgresContainer(ctx)
	require.NoError(t, err)
	defer func() { _ = pg.Stop(ctx) }()

	cfg := pg.GetConfig()
	client := NewClient(cfg)

	err = client.CloneDatabase(ctx, cfg.Database, cfg.Database, StrategyTemplate)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}
