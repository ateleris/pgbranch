package core

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/le-vlad/pgbranch/internal/testutil"
	"github.com/le-vlad/pgbranch/pkg/config"
	"github.com/le-vlad/pgbranch/pkg/postgres"
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


// TestCheckout_MissingSnapshotForOneDatabase_NoDatabaseTouched verifies the
// pre-flight check: if a branch is missing a snapshot for any configured
// database, Checkout fails with a clear error naming it and does not touch
// any working database -- not even the ones that do have a snapshot.
func TestCheckout_MissingSnapshotForOneDatabase_NoDatabaseTouched(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()

	pg, err := testutil.StartPostgresContainer(ctx)
	require.NoError(t, err)
	defer func() { _ = pg.Stop(ctx) }()

	testDir := testutil.SetupTestDir(t)
	defer testDir.Cleanup(t)

	baseCfg := pg.GetConfig()
	const (
		dbA = "pgbranch_test"
		dbB = "pgbranch_test_b"
	)

	adminConn, err := pgx.Connect(ctx, baseCfg.ConnectionURLForDB("postgres"))
	require.NoError(t, err)
	_, err = adminConn.Exec(ctx, "CREATE DATABASE "+dbB)
	require.NoError(t, err)
	require.NoError(t, adminConn.Close(ctx))

	cfg := &config.Config{
		Databases: []config.DatabaseConfig{
			{Name: dbA, Strategy: "template"},
			{Name: dbB, Strategy: "template"},
		},
		Host:           baseCfg.Host,
		Port:           baseCfg.Port,
		User:           baseCfg.User,
		Password:       baseCfg.Password,
		BaselineBranch: "main",
	}
	require.NoError(t, Initialize(testDir.Path, cfg))

	require.NoError(t, execOn(ctx, cfg, dbA, "CREATE TABLE a_marker (id INT)"))
	require.NoError(t, execOn(ctx, cfg, dbA, "INSERT INTO a_marker VALUES (1)"))
	require.NoError(t, execOn(ctx, cfg, dbB, "CREATE TABLE b_marker (id INT)"))
	require.NoError(t, execOn(ctx, cfg, dbB, "INSERT INTO b_marker VALUES (1)"))

	brancher, err := Open(testDir.Path)
	require.NoError(t, err)

	require.NoError(t, brancher.CreateBranch(ctx, "main", ""))
	brancher.Metadata.CurrentBranch = "main"
	require.NoError(t, brancher.Metadata.Save())

	// A distinct, independent snapshot for dbA, so that "broken"'s dbA
	// snapshot is not the very same database as "main"'s (which Checkout's
	// own auto-save-current-branch step touches and would otherwise
	// confound this test).
	require.NoError(t, brancher.Client.CloneDatabase(ctx, dbA, "broken_dbA_snap", postgres.StrategyTemplate))
	defer func() { _ = brancher.Client.DropDatabaseByName(ctx, "broken_dbA_snap") }()

	// A branch with a snapshot for dbA but not dbB (e.g. metadata
	// corruption, or a config change adding a database after the branch
	// was created).
	brancher.Metadata.AddBranch("broken", "", map[string]string{dbA: "broken_dbA_snap"})
	require.NoError(t, brancher.Metadata.Save())

	// Diverge the working databases so we can tell if Checkout touched them.
	require.NoError(t, execOn(ctx, cfg, dbA, "INSERT INTO a_marker VALUES (2)"))
	require.NoError(t, execOn(ctx, cfg, dbB, "INSERT INTO b_marker VALUES (2)"))

	err = brancher.Checkout(ctx, "broken")
	require.Error(t, err)
	assert.Contains(t, err.Error(), dbB)

	assert.Equal(t, "main", brancher.Metadata.CurrentBranch, "checkout must not have proceeded")

	countA, err := countOn(ctx, cfg, dbA, "a_marker")
	require.NoError(t, err)
	assert.Equal(t, 2, countA, "database with a snapshot must not be touched when another database is missing one")

	countB, err := countOn(ctx, cfg, dbB, "b_marker")
	require.NoError(t, err)
	assert.Equal(t, 2, countB)
}

// TestCheckout_SecondDatabasePrepareFails_FirstDatabaseUntouched verifies
// the two-phase restructuring: every database's scratch clone is built
// (Prepare) before any working database is actually swapped (Commit). If
// preparing a later database fails, an earlier database in the same
// checkout -- even though it would have been perfectly fine to swap -- must
// still be untouched, since the operation as a whole did not succeed.
func TestCheckout_SecondDatabasePrepareFails_FirstDatabaseUntouched(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()

	pg, err := testutil.StartPostgresContainer(ctx)
	require.NoError(t, err)
	defer func() { _ = pg.Stop(ctx) }()

	testDir := testutil.SetupTestDir(t)
	defer testDir.Cleanup(t)

	baseCfg := pg.GetConfig()
	const (
		dbA = "pgbranch_test"
		dbB = "pgbranch_test_b"
	)

	adminConn, err := pgx.Connect(ctx, baseCfg.ConnectionURLForDB("postgres"))
	require.NoError(t, err)
	_, err = adminConn.Exec(ctx, "CREATE DATABASE "+dbB)
	require.NoError(t, err)
	require.NoError(t, adminConn.Close(ctx))

	cfg := &config.Config{
		Databases: []config.DatabaseConfig{
			{Name: dbA, Strategy: "template"},
			{Name: dbB, Strategy: "template"},
		},
		Host:           baseCfg.Host,
		Port:           baseCfg.Port,
		User:           baseCfg.User,
		Password:       baseCfg.Password,
		BaselineBranch: "main",
	}
	require.NoError(t, Initialize(testDir.Path, cfg))

	require.NoError(t, execOn(ctx, cfg, dbA, "CREATE TABLE a_marker (id INT)"))
	require.NoError(t, execOn(ctx, cfg, dbA, "INSERT INTO a_marker VALUES (1)"))
	require.NoError(t, execOn(ctx, cfg, dbB, "CREATE TABLE b_marker (id INT)"))
	require.NoError(t, execOn(ctx, cfg, dbB, "INSERT INTO b_marker VALUES (1)"))

	brancher, err := Open(testDir.Path)
	require.NoError(t, err)

	require.NoError(t, brancher.CreateBranch(ctx, "main", ""))
	brancher.Metadata.CurrentBranch = "main"
	require.NoError(t, brancher.Metadata.Save())

	// A second branch, with its own independent snapshots, that we switch
	// to and leave current -- so that Checkout's own auto-save-current-
	// branch step (which happens before the "target" checkout below) saves
	// *this* branch's snapshots, not "main"'s, and so does not incidentally
	// recreate the dangling snapshot database "target" is about to
	// reference.
	require.NoError(t, brancher.CreateBranch(ctx, "feature", "main"))
	require.NoError(t, brancher.Checkout(ctx, "feature"))

	// A "target" branch with a valid, independent snapshot for dbA, but
	// whose dbB snapshot points at a database that has since been dropped
	// -- Prepare (the clone step) for dbB will fail.
	require.NoError(t, brancher.Client.CloneDatabase(ctx, dbA, "target_dbA_snap", postgres.StrategyTemplate))
	defer func() { _ = brancher.Client.DropDatabaseByName(ctx, "target_dbA_snap") }()

	mainBranch, ok := brancher.Metadata.GetBranch("main")
	require.True(t, ok)
	require.NoError(t, brancher.Client.DropDatabaseByName(ctx, mainBranch.Snapshots[dbB]))

	brancher.Metadata.AddBranch("target", "", map[string]string{
		dbA: "target_dbA_snap",
		dbB: mainBranch.Snapshots[dbB], // now dangling
	})
	require.NoError(t, brancher.Metadata.Save())

	require.NoError(t, execOn(ctx, cfg, dbA, "INSERT INTO a_marker VALUES (2)"))

	err = brancher.Checkout(ctx, "target")
	require.Error(t, err, "checkout must fail because dbB's snapshot cannot be cloned")

	assert.Equal(t, "feature", brancher.Metadata.CurrentBranch, "checkout must not have completed")

	countA, err := countOn(ctx, cfg, dbA, "a_marker")
	require.NoError(t, err)
	assert.Equal(t, 2, countA, "dbA must be untouched: its scratch clone must not be committed before dbB's clone is known to succeed")
}
