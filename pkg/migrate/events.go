package migrate

// Event reports the progress of a migration. The set of implementations is
// closed: they are the concrete event types declared in this package.
//
// Events carry no presentation logic. Rendering them, whether as a progress
// bar, a log line or a metrics counter, is the caller's concern.
type Event interface {
	migrateEvent()
}

// Phase names reported by PhaseEvent for the fixed stages of a migration.
// Reconnection attempts report a phase that is not one of these constants.
const (
	PhaseValidate  = "validate"
	PhaseSchema    = "schema"
	PhaseSetup     = "setup"
	PhaseSnapshot  = "snapshot"
	PhaseStreaming = "streaming"
)

// PhaseEvent reports that the migration moved to a new phase.
type PhaseEvent struct {
	Phase string
}

// TableInitEvent reports the total row count of a table about to be copied.
type TableInitEvent struct {
	Table     string
	TotalRows int64
}

// TableProgressEvent reports rows copied for a table since the previous event.
type TableProgressEvent struct {
	Table     string
	RowsDelta int64
}

// TableDoneEvent reports that a table finished copying.
type TableDoneEvent struct {
	Table string
}

// StreamingEvent reports replication progress while tailing the write-ahead
// log, with the running totals of applied row changes.
type StreamingEvent struct {
	LSN     string
	Inserts int64
	Updates int64
	Deletes int64
}

func (PhaseEvent) migrateEvent()         {}
func (TableInitEvent) migrateEvent()     {}
func (TableProgressEvent) migrateEvent() {}
func (TableDoneEvent) migrateEvent()     {}
func (StreamingEvent) migrateEvent()     {}
