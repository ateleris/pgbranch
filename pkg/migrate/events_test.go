package migrate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A caller that never drains Events must not be able to stall the migration,
// so send drops events once the buffer is full rather than blocking.
func TestSendDoesNotBlockWhenNobodyReads(t *testing.T) {
	m := NewMigrator(&Config{}, false, RunFull)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < cap(m.events)*3; i++ {
			m.send(PhaseEvent{Phase: PhaseSnapshot})
		}
	}()

	select {
	case <-done:
	case <-t.Context().Done():
		t.Fatal("send blocked when the event buffer was full")
	}

	assert.Len(t, m.events, cap(m.events), "buffer should be full, with the excess dropped")
}

func TestEventsDeliversTypedEvents(t *testing.T) {
	m := NewMigrator(&Config{}, false, RunFull)

	m.send(PhaseEvent{Phase: PhaseStreaming})
	m.send(TableInitEvent{Table: "public.users", TotalRows: 42})
	close(m.events)

	var got []Event
	for e := range m.Events() {
		got = append(got, e)
	}

	require.Len(t, got, 2)
	assert.Equal(t, PhaseEvent{Phase: PhaseStreaming}, got[0])
	assert.Equal(t, TableInitEvent{Table: "public.users", TotalRows: 42}, got[1])
}
