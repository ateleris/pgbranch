package migrate

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type RunMode int

const (
	RunFull RunMode = iota
	RunSchemaOnly
	RunSnapshotOnly
)

type Migrator struct {
	config     *Config
	tables     []string
	checkpoint *Checkpoint
	replicator *Replicator
	keepSlot   bool
	mode       RunMode
	events     chan Event
}

func NewMigrator(cfg *Config, keepSlot bool, mode RunMode) *Migrator {
	return &Migrator{
		config:   cfg,
		keepSlot: keepSlot,
		mode:     mode,
		events:   make(chan Event, 100),
	}
}

// Events returns the channel on which the migration reports progress. It is
// closed when Run returns.
//
// Events are dropped rather than blocking the migration when the receiver
// falls behind, so a caller that never reads this channel is safe.
func (m *Migrator) Events() <-chan Event {
	return m.events
}

// Run executes the migration and blocks until it finishes, the context is
// cancelled, or it fails. Progress is reported on the Events channel; how it
// is displayed is up to the caller.
func (m *Migrator) Run(ctx context.Context) error {
	return m.executeMigration(ctx)
}

func (m *Migrator) executeMigration(ctx context.Context) error {
	defer close(m.events)

	cp, err := LoadCheckpoint(m.config.CheckpointPath())
	if err != nil {
		return fmt.Errorf("failed to load checkpoint: %w", err)
	}
	m.checkpoint = cp
	cp.SlotName = m.config.SlotName
	cp.PublicationName = m.config.PublicationName

	m.send(PhaseEvent{Phase: PhaseValidate})

	sourceConn, err := pgx.Connect(ctx, m.config.Source.ConnectionURL())
	if err != nil {
		return fmt.Errorf("failed to connect to source: %w", err)
	}
	defer func() { _ = sourceConn.Close(ctx) }()

	if err := ValidateSource(ctx, sourceConn); err != nil {
		return fmt.Errorf("source validation failed: %w", err)
	}

	targetConn, err := pgx.Connect(ctx, m.config.Target.ConnectionURL())
	if err != nil {
		return fmt.Errorf("failed to connect to target: %w", err)
	}
	defer func() { _ = targetConn.Close(ctx) }()

	if err := ValidateTarget(ctx, targetConn); err != nil {
		return fmt.Errorf("target validation failed: %w", err)
	}

	tables, err := ResolveTables(ctx, sourceConn, m.config.Tables)
	if err != nil {
		return err
	}
	m.tables = tables
	cp.InitTables(tables)

	_ = sourceConn.Close(ctx)
	_ = targetConn.Close(ctx)

	if !cp.SchemaApplied {
		m.send(PhaseEvent{Phase: PhaseSchema})

		srcConn, err := pgx.Connect(ctx, m.config.Source.ConnectionURL())
		if err != nil {
			return fmt.Errorf("failed to connect to source for schema: %w", err)
		}

		tgtConn, err := pgx.Connect(ctx, m.config.Target.ConnectionURL())
		if err != nil {
			_ = srcConn.Close(ctx)
			return fmt.Errorf("failed to connect to target for schema: %w", err)
		}

		err = CopySchema(ctx, srcConn, tgtConn, tables, m.config.Source.Database)
		_ = srcConn.Close(ctx)
		_ = tgtConn.Close(ctx)

		if err != nil {
			return fmt.Errorf("schema copy failed: %w", err)
		}

		cp.SchemaApplied = true
		_ = cp.Save()
	}

	if m.mode == RunSchemaOnly {
		return nil
	}

	m.replicator = NewReplicator(m.config, tables, cp, m.events)
	if err := m.replicator.Connect(ctx); err != nil {
		return err
	}
	defer m.cleanup(ctx)

	m.send(PhaseEvent{Phase: PhaseSetup})

	snapshotName, consistentLSN, err := m.replicator.Setup(ctx)
	if err != nil {
		return fmt.Errorf("replication setup failed: %w", err)
	}

	cp.SnapshotName = snapshotName
	cp.ConsistentLSN = consistentLSN.String()
	_ = cp.Save()

	if !cp.IsSnapshotComplete() {
		m.send(PhaseEvent{Phase: PhaseSnapshot})
		cp.Phase = "snapshot"
		_ = cp.Save()

		if err := m.replicator.RunSnapshot(ctx, snapshotName); err != nil {
			return fmt.Errorf("snapshot failed: %w", err)
		}
	}

	if m.mode == RunSnapshotOnly {
		return nil
	}

	m.send(PhaseEvent{Phase: PhaseStreaming})
	cp.Phase = "streaming"
	_ = cp.Save()

	if err := m.replicator.StartStreaming(ctx); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("streaming failed: %w", err)
	}

	return nil
}

func (m *Migrator) cleanup(ctx context.Context) {
	if m.replicator == nil {
		return
	}

	if !m.keepSlot {
		_ = m.replicator.Cleanup(ctx)
		_ = m.checkpoint.Delete()
	} else {
		_ = m.checkpoint.Save()
	}

	_ = m.replicator.Close()
}

func (m *Migrator) send(e Event) {
	select {
	case m.events <- e:
	default:
	}
}
