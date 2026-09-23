package postgres

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/le-vlad/pgbranch/internal/testutil"
)

func TestParseStrategy(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    Strategy
		wantErr bool
	}{
		{name: "empty defaults to auto", in: "", want: StrategyAuto},
		{name: "auto", in: "auto", want: StrategyAuto},
		{name: "template", in: "template", want: StrategyTemplate},
		{name: "dump", in: "dump", want: StrategyDump},
		{name: "invalid", in: "bogus", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseStrategy(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolveStrategy_Auto(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()

	t.Run("plain database resolves to template", func(t *testing.T) {
		pg, err := testutil.StartPostgresContainer(ctx)
		require.NoError(t, err)
		defer func() { _ = pg.Stop(ctx) }()

		cfg := pg.GetConfig()
		client := NewClient(cfg)

		resolved, err := client.ResolveStrategy(ctx, cfg.Database, StrategyAuto)
		require.NoError(t, err)
		assert.Equal(t, StrategyTemplate, resolved)
	})

	t.Run("timescale database resolves to dump", func(t *testing.T) {
		cfg := timescaleConfig(t, ctx)
		client := NewClient(cfg)

		mustExecSQL(t, ctx, cfg, cfg.Database, `CREATE EXTENSION IF NOT EXISTS timescaledb;`)

		resolved, err := client.ResolveStrategy(ctx, cfg.Database, StrategyAuto)
		require.NoError(t, err)
		assert.Equal(t, StrategyDump, resolved)
	})
}

func TestHasExtension(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()

	pg, err := testutil.StartPostgresContainer(ctx)
	require.NoError(t, err)
	defer func() { _ = pg.Stop(ctx) }()

	cfg := pg.GetConfig()
	client := NewClient(cfg)

	has, err := client.HasExtension(ctx, cfg.Database, "timescaledb")
	require.NoError(t, err)
	assert.False(t, has)

	has, err = client.HasExtension(ctx, cfg.Database, "plpgsql")
	require.NoError(t, err)
	assert.True(t, has)
}
