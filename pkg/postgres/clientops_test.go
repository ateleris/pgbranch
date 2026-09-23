package postgres

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/le-vlad/pgbranch/internal/testutil"
)

func TestRenameDatabase(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()

	pg, err := testutil.StartPostgresContainer(ctx)
	require.NoError(t, err)
	defer func() { _ = pg.Stop(ctx) }()

	cfg := pg.GetConfig()
	client := NewClient(cfg)

	from := "rename_from_db"
	to := "rename_to_db"
	require.NoError(t, client.CreateEmptyDatabase(ctx, from))
	defer func() { _ = client.DropDatabaseByName(ctx, to) }()
	defer func() { _ = client.DropDatabaseByName(ctx, from) }()

	err = client.RenameDatabase(ctx, from, to)
	require.NoError(t, err)

	fromExists, err := client.Exists(ctx, from)
	require.NoError(t, err)
	assert.False(t, fromExists)

	toExists, err := client.Exists(ctx, to)
	require.NoError(t, err)
	assert.True(t, toExists)
}

func TestExistsAndSize(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()

	pg, err := testutil.StartPostgresContainer(ctx)
	require.NoError(t, err)
	defer func() { _ = pg.Stop(ctx) }()

	cfg := pg.GetConfig()
	client := NewClient(cfg)

	exists, err := client.Exists(ctx, cfg.Database)
	require.NoError(t, err)
	assert.True(t, exists)

	exists, err = client.Exists(ctx, "totally_missing_db")
	require.NoError(t, err)
	assert.False(t, exists)

	size, err := client.Size(ctx, cfg.Database)
	require.NoError(t, err)
	assert.Greater(t, size, int64(0))
}
