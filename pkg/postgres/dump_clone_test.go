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

	dst := "clone_timescale_dst"
	err := client.CloneDatabase(ctx, cfg.Database, dst, StrategyAuto)
	require.NoError(t, err)
	defer func() { _ = client.DropDatabaseByName(ctx, dst) }()

	assert.Equal(t, 50, mustCountRows(t, ctx, cfg, dst, "metrics"))

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
