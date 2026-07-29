package cli

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/le-vlad/pgbranch/pkg/migrate"
)

// The TUI consumes the library's typed events directly as bubbletea messages.
// These tests pin that wiring, since pkg/migrate no longer knows the TUI exists.

func apply(m tuiModel, msgs ...any) tuiModel {
	for _, msg := range msgs {
		updated, _ := m.Update(msg)
		m = updated.(tuiModel)
	}
	return m
}

func TestTUITracksPhase(t *testing.T) {
	m := apply(newTUIModel(), migrate.PhaseEvent{Phase: migrate.PhaseSnapshot})

	assert.Equal(t, migrate.PhaseSnapshot, m.phase)
}

func TestTUIAccumulatesTableProgress(t *testing.T) {
	m := apply(newTUIModel(),
		migrate.TableInitEvent{Table: "public.users", TotalRows: 100},
		migrate.TableProgressEvent{Table: "public.users", RowsDelta: 30},
		migrate.TableProgressEvent{Table: "public.users", RowsDelta: 20},
	)

	require.Len(t, m.tables, 1)
	assert.Equal(t, "public.users", m.tables[0].name)
	assert.Equal(t, int64(100), m.tables[0].total)
	assert.Equal(t, int64(50), m.tables[0].copied)
	assert.False(t, m.tables[0].done)
}

// A finished table is shown as fully copied even if the progress events did
// not add up to the estimated row count.
func TestTUIDoneSnapsToTotal(t *testing.T) {
	m := apply(newTUIModel(),
		migrate.TableInitEvent{Table: "public.users", TotalRows: 100},
		migrate.TableProgressEvent{Table: "public.users", RowsDelta: 12},
		migrate.TableDoneEvent{Table: "public.users"},
	)

	require.Len(t, m.tables, 1)
	assert.True(t, m.tables[0].done)
	assert.Equal(t, int64(100), m.tables[0].copied)
}

func TestTUIIgnoresEventsForUnknownTables(t *testing.T) {
	m := apply(newTUIModel(),
		migrate.TableProgressEvent{Table: "never.announced", RowsDelta: 5},
		migrate.TableDoneEvent{Table: "never.announced"},
	)

	assert.Empty(t, m.tables)
}

func TestTUITracksStreamingCounters(t *testing.T) {
	m := apply(newTUIModel(), migrate.StreamingEvent{
		LSN: "0/1A2B3C4", Inserts: 7, Updates: 3, Deletes: 1,
	})

	assert.Equal(t, "0/1A2B3C4", m.streaming.lsn)
	assert.Equal(t, int64(7), m.streaming.inserts)
	assert.Equal(t, int64(3), m.streaming.updates)
	assert.Equal(t, int64(1), m.streaming.deletes)
}

func TestTUIRecordsMigrationFailure(t *testing.T) {
	wantErr := errors.New("replication slot already exists")

	m := apply(newTUIModel(), migrationDoneMsg{Err: wantErr})

	assert.True(t, m.quitting)
	assert.ErrorIs(t, m.err, wantErr)
}

func TestTUIRendersWithoutPanicking(t *testing.T) {
	m := apply(newTUIModel(),
		migrate.PhaseEvent{Phase: migrate.PhaseSnapshot},
		migrate.TableInitEvent{Table: "public.users", TotalRows: 100},
		migrate.TableProgressEvent{Table: "public.users", RowsDelta: 40},
		migrate.PhaseEvent{Phase: migrate.PhaseStreaming},
		migrate.StreamingEvent{LSN: "0/1A2B3C4", Inserts: 7},
	)

	assert.NotEmpty(t, m.View())
}
