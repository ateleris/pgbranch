package postgres

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckDumpToolsAvailable(t *testing.T) {
	// pg_dump/pg_restore are expected to be installed in the dev/CI
	// environment (documented in the plan); this just exercises the happy
	// path of the LookPath check without needing docker.
	err := checkDumpToolsAvailable()
	if err != nil {
		t.Skipf("pg_dump/pg_restore not available: %v", err)
	}
}

func TestCloneDatabase_Dump(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()

	cfg := dump14Config(t, ctx)
	client := NewClient(cfg)

	mustExecSQL(t, ctx, cfg, cfg.Database, `
		CREATE TABLE widgets (id SERIAL PRIMARY KEY, name TEXT);
		INSERT INTO widgets (name) VALUES ('w1'), ('w2'), ('w3'), ('w4');
	`)

	dst := "clone_dump_dst"
	err := client.CloneDatabase(ctx, cfg.Database, dst, StrategyDump)
	require.NoError(t, err)
	defer func() { _ = client.DropDatabaseByName(ctx, dst) }()

	assert.Equal(t, 4, mustCountRows(t, ctx, cfg, dst, "widgets"))

	srcCount, err := client.TableCount(ctx, cfg.Database)
	require.NoError(t, err)
	dstCount, err := client.TableCount(ctx, dst)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, dstCount, srcCount)
}

func TestCloneDatabase_TimescaleHypertable(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()

	cfg := timescaleConfig(t, ctx)
	client := NewClient(cfg)

	mustExecSQL(t, ctx, cfg, cfg.Database, `
		CREATE EXTENSION IF NOT EXISTS timescaledb;
		CREATE TABLE metrics (time TIMESTAMPTZ NOT NULL, value DOUBLE PRECISION);
		SELECT create_hypertable('metrics', 'time');
		INSERT INTO metrics (time, value)
		SELECT now() - (i || ' minutes')::interval, i
		FROM generate_series(1, 50) AS i;
	`)

	srcVersion, err := client.ExtensionVersion(ctx, cfg.Database, "timescaledb")
	require.NoError(t, err)

	dst := "clone_timescale_dst"
	err = client.CloneDatabase(ctx, cfg.Database, dst, StrategyAuto)
	require.NoError(t, err)
	defer func() { _ = client.DropDatabaseByName(ctx, dst) }()

	assert.Equal(t, 50, mustCountRows(t, ctx, cfg, dst, "metrics"))

	dstVersion, err := client.ExtensionVersion(ctx, dst, "timescaledb")
	require.NoError(t, err)
	assert.Equal(t, srcVersion, dstVersion, "clone must reproduce the source's timescaledb version")

	conn, err := pgx.Connect(ctx, cfg.ConnectionURLForDB(dst))
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }()

	var isHypertable bool
	err = conn.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM timescaledb_information.hypertables WHERE hypertable_name = 'metrics')`,
	).Scan(&isHypertable)
	require.NoError(t, err)
	assert.True(t, isHypertable, "metrics should still be a hypertable after clone")
}

// TestCloneDatabase_TimescaleStaleExtensionVersion verifies that cloning a
// database whose installed timescaledb version is older than the version
// the server currently loads by default fails with a clear, actionable
// error, instead of either:
//   - silently creating the destination's extension at the newest version
//     (the previous behavior) while copying over data shaped for the older
//     version, which TimescaleDB is not guaranteed to tolerate, or
//   - pinning the destination to the stale version via a VERSION clause,
//     which (verified against a real timescaledb container) makes
//     TimescaleDB's own timescaledb_post_restore() fail with an opaque
//     "catalog version mismatch" error, because its restore hooks require
//     the extension to already be at the version the loaded library
//     expects.
//
// The correct fix is to detect the mismatch up front and tell the user to
// run `ALTER EXTENSION timescaledb UPDATE` on the source first.
func TestCloneDatabase_TimescaleStaleExtensionVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()

	cfg := timescaleConfig(t, ctx)
	client := NewClient(cfg)

	conn, err := pgx.Connect(ctx, cfg.ConnectionURLForDB(cfg.Database))
	require.NoError(t, err)
	var defaultVersion string
	err = conn.QueryRow(ctx,
		"SELECT default_version FROM pg_available_extensions WHERE name = 'timescaledb'",
	).Scan(&defaultVersion)
	require.NoError(t, err)
	var olderVersion string
	err = conn.QueryRow(ctx,
		"SELECT version FROM pg_available_extension_versions WHERE name = 'timescaledb' AND version <> $1 ORDER BY version LIMIT 1",
		defaultVersion,
	).Scan(&olderVersion)
	require.NoError(t, err)
	require.NoError(t, conn.Close(ctx))
	require.NotEqual(t, defaultVersion, olderVersion)

	mustExecSQL(t, ctx, cfg, cfg.Database, "DROP EXTENSION IF EXISTS timescaledb; CREATE EXTENSION timescaledb VERSION '"+olderVersion+"'")

	err = client.CloneDatabase(ctx, cfg.Database, "clone_timescale_stale_dst", StrategyAuto)
	require.Error(t, err)
	assert.Contains(t, err.Error(), olderVersion)
	assert.Contains(t, err.Error(), defaultVersion)
	assert.Contains(t, err.Error(), "ALTER EXTENSION")

	exists, err := client.Exists(ctx, "clone_timescale_stale_dst")
	require.NoError(t, err)
	assert.False(t, exists, "no destination database should be left behind after the guard fails")
}
