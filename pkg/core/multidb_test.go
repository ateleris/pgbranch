package core

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/le-vlad/pgbranch/internal/testutil"
	"github.com/le-vlad/pgbranch/pkg/config"
)

// execOn runs sql against dbName using cfg's connection settings.
func execOn(ctx context.Context, cfg *config.Config, dbName, sql string) error {
	conn, err := pgx.Connect(ctx, cfg.ConnectionURLForDB(dbName))
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(ctx) }()

	_, err = conn.Exec(ctx, sql)
	return err
}

func countOn(ctx context.Context, cfg *config.Config, dbName, table string) (int, error) {
	conn, err := pgx.Connect(ctx, cfg.ConnectionURLForDB(dbName))
	if err != nil {
		return 0, err
	}
	defer func() { _ = conn.Close(ctx) }()

	var count int
	err = conn.QueryRow(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count)
	return count, err
}

// TestMultiDatabaseWorkflow exercises a workspace with two databases, one
// using the dump strategy, through create/checkout/reset/prune --gone.
func TestMultiDatabaseWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()

	// pg_dump on the machine running these tests is v14, so the dump
	// strategy needs a matching server.
	pg, err := testutil.StartPostgresContainerImage(ctx, "postgres:14-alpine")
	require.NoError(t, err)
	defer func() { _ = pg.Stop(ctx) }()

	testDir := testutil.SetupTestDir(t)
	defer testDir.Cleanup(t)

	baseCfg := pg.GetConfig()

	const (
		appDB      = "pgbranch_test"
		identityDB = "pgbranch_test_identity"
	)

	adminConn, err := pgx.Connect(ctx, baseCfg.ConnectionURLForDB("postgres"))
	require.NoError(t, err)
	_, err = adminConn.Exec(ctx, "CREATE DATABASE "+identityDB)
	require.NoError(t, err)
	require.NoError(t, adminConn.Close(ctx))

	cfg := &config.Config{
		Databases: []config.DatabaseConfig{
			{Name: appDB, Strategy: "template"},
			{Name: identityDB, Strategy: "dump"},
		},
		Host:           baseCfg.Host,
		Port:           baseCfg.Port,
		User:           baseCfg.User,
		Password:       baseCfg.Password,
		BaselineBranch: "main",
	}

	require.NoError(t, Initialize(testDir.Path, cfg))

	require.NoError(t, execOn(ctx, cfg, appDB, "CREATE TABLE widgets (id SERIAL PRIMARY KEY, name TEXT)"))
	require.NoError(t, execOn(ctx, cfg, appDB, "INSERT INTO widgets (name) VALUES ('base-widget')"))
	require.NoError(t, execOn(ctx, cfg, identityDB, "CREATE TABLE users (id SERIAL PRIMARY KEY, email TEXT)"))
	require.NoError(t, execOn(ctx, cfg, identityDB, "INSERT INTO users (email) VALUES ('base@example.com')"))

	brancher, err := Open(testDir.Path)
	require.NoError(t, err)

	// Create and check out the baseline branch.
	require.NoError(t, brancher.CreateBranch(ctx, "main", ""))
	brancher.Metadata.CurrentBranch = "main"
	require.NoError(t, brancher.Metadata.Save())

	mainBranch, ok := brancher.Metadata.GetBranch("main")
	require.True(t, ok)
	assert.Len(t, mainBranch.Snapshots, 2)

	// Create "feature" from "main" and switch to it.
	require.NoError(t, brancher.CreateBranch(ctx, "feature", "main"))
	require.NoError(t, brancher.Checkout(ctx, "feature"))
	assert.Equal(t, "feature", brancher.Metadata.CurrentBranch)

	// Modify feature data in both databases.
	require.NoError(t, execOn(ctx, cfg, appDB, "INSERT INTO widgets (name) VALUES ('feature-widget')"))
	require.NoError(t, execOn(ctx, cfg, identityDB, "INSERT INTO users (email) VALUES ('feature@example.com')"))

	widgetCount, err := countOn(ctx, cfg, appDB, "widgets")
	require.NoError(t, err)
	assert.Equal(t, 2, widgetCount)

	// Checkout baseline: data reverts.
	require.NoError(t, brancher.Checkout(ctx, "main"))

	widgetCount, err = countOn(ctx, cfg, appDB, "widgets")
	require.NoError(t, err)
	assert.Equal(t, 1, widgetCount)

	userCount, err := countOn(ctx, cfg, identityDB, "users")
	require.NoError(t, err)
	assert.Equal(t, 1, userCount)

	// Checkout feature again: modification is back.
	require.NoError(t, brancher.Checkout(ctx, "feature"))

	widgetCount, err = countOn(ctx, cfg, appDB, "widgets")
	require.NoError(t, err)
	assert.Equal(t, 2, widgetCount)

	userCount, err = countOn(ctx, cfg, identityDB, "users")
	require.NoError(t, err)
	assert.Equal(t, 2, userCount)

	// Reset back to baseline: discards the feature modification and
	// overwrites feature's own snapshot with the reset state.
	require.NoError(t, brancher.Reset(ctx, "main"))

	widgetCount, err = countOn(ctx, cfg, appDB, "widgets")
	require.NoError(t, err)
	assert.Equal(t, 1, widgetCount)

	userCount, err = countOn(ctx, cfg, identityDB, "users")
	require.NoError(t, err)
	assert.Equal(t, 1, userCount)

	// Switch back to baseline: prune --gone never touches the current
	// branch, and "feature" must not be current to be prunable.
	require.NoError(t, brancher.Checkout(ctx, "main"))

	// prune --gone: "feature"'s git branch no longer exists locally, "main"
	// is the baseline and must never be pruned.
	gone := brancher.GoneBranches([]string{"main"})
	assert.Equal(t, []string{"feature"}, gone)

	deleted, errs := brancher.PruneBranches(ctx, gone)
	assert.Empty(t, errs)
	assert.Equal(t, []string{"feature"}, deleted)
	assert.False(t, brancher.Metadata.BranchExists("feature"))

	branches := brancher.ListBranches()
	require.Len(t, branches, 1)
	assert.Equal(t, "main", branches[0].Name)
}
