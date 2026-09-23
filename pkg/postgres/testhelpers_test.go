package postgres

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/le-vlad/pgbranch/internal/testutil"
	"github.com/le-vlad/pgbranch/pkg/config"
)

// dump14Config returns a config pointed at a fresh postgres:14-alpine
// container, matching the locally installed pg_dump/pg_restore version.
func dump14Config(t *testing.T, ctx context.Context) *config.Config {
	t.Helper()

	pg, err := testutil.StartPostgresContainerImage(ctx, "postgres:14-alpine")
	if err != nil {
		t.Skipf("skipping: could not start postgres:14-alpine container: %v", err)
	}
	t.Cleanup(func() {
		_ = pg.Stop(context.Background())
	})
	return pg.GetConfig()
}

func timescaleConfig(t *testing.T, ctx context.Context) *config.Config {
	t.Helper()

	pg, err := testutil.StartPostgresContainerImage(ctx, "timescale/timescaledb:latest-pg14")
	if err != nil {
		t.Skipf("skipping: could not start timescaledb container: %v", err)
	}
	t.Cleanup(func() {
		_ = pg.Stop(context.Background())
	})
	return pg.GetConfig()
}

func mustExecSQL(t *testing.T, ctx context.Context, cfg *config.Config, db, sql string) {
	t.Helper()
	conn, err := pgx.Connect(ctx, cfg.ConnectionURLForDB(db))
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }()
	_, err = conn.Exec(ctx, sql)
	require.NoError(t, err)
}

func mustCountRows(t *testing.T, ctx context.Context, cfg *config.Config, db, table string) int {
	t.Helper()
	conn, err := pgx.Connect(ctx, cfg.ConnectionURLForDB(db))
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }()

	var count int
	err = conn.QueryRow(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count)
	require.NoError(t, err)
	return count
}
