package cli

import (
	"context"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/le-vlad/pgbranch/pkg/migrate"
)

func newMigrateCmd() *cobra.Command {
	var (
		configPath   string
		keepSlot     bool
		schemaOnly   bool
		snapshotOnly bool
	)

	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Continuous PG-to-PG database migration via logical replication",
		Long: `Migrate a PostgreSQL database to another PostgreSQL instance using logical replication.

Reads a YAML configuration file describing source and target databases,
then performs: schema copy → initial data snapshot → live WAL streaming.

The migration shows table-by-table progress and supports resume on interruption.

Example:
  pgbranch migrate -c migration.yaml
  pgbranch migrate -c migration.yaml --schema-only
  pgbranch migrate -c migration.yaml --snapshot-only
  pgbranch migrate -c migration.yaml --keep

Requirements:
  - Source PostgreSQL must have wal_level=logical
  - Source user must have REPLICATION privilege
  - Tables must have a PRIMARY KEY (for UPDATE/DELETE replication)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if configPath == "" {
				return fmt.Errorf("--config flag is required")
			}

			cfg, err := migrate.LoadConfig(configPath)
			if err != nil {
				return err
			}

			mode := migrate.RunFull
			if schemaOnly {
				mode = migrate.RunSchemaOnly
			} else if snapshotOnly {
				mode = migrate.RunSnapshotOnly
			}

			migrator := migrate.NewMigrator(cfg, keepSlot, mode)

			if term.IsTerminal(int(os.Stdout.Fd())) {
				return runMigrationWithTUI(cmd.Context(), migrator)
			}
			return runMigrationWithPlainLog(cmd.Context(), migrator)
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to YAML configuration file (required)")
	cmd.Flags().BoolVar(&keepSlot, "keep", false, "Keep replication slot and publication on exit (for resume)")
	cmd.Flags().BoolVar(&schemaOnly, "schema-only", false, "Copy schema only, then exit")
	cmd.Flags().BoolVar(&snapshotOnly, "snapshot-only", false, "Copy schema and initial data, then exit (no streaming)")
	_ = cmd.MarkFlagRequired("config")

	return cmd
}

// runMigrationWithTUI renders migration progress in a full screen bubbletea
// program. Deciding to take over the terminal is a CLI concern, which is why
// it lives here rather than in pkg/migrate.
func runMigrationWithTUI(ctx context.Context, migrator *migrate.Migrator) error {
	program := tea.NewProgram(newTUIModel(), tea.WithAltScreen())

	go func() {
		program.Send(migrationDoneMsg{Err: migrator.Run(ctx)})
	}()

	go func() {
		for event := range migrator.Events() {
			program.Send(event)
		}
	}()

	finalModel, err := program.Run()
	if err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	if m, ok := finalModel.(tuiModel); ok && m.err != nil {
		return m.err
	}

	return nil
}

// runMigrationWithPlainLog prints migration progress as plain log lines, for
// when stdout is not a terminal.
func runMigrationWithPlainLog(ctx context.Context, migrator *migrate.Migrator) error {
	logger := newPlainLogger()

	go func() {
		for event := range migrator.Events() {
			switch e := event.(type) {
			case migrate.PhaseEvent:
				logger.SetPhase(e.Phase)
			case migrate.TableInitEvent:
				logger.TableInit(e.Table, e.TotalRows)
			case migrate.TableDoneEvent:
				logger.TableDone(e.Table)
			case migrate.StreamingEvent:
				logger.StreamingUpdate(e.LSN, e.Inserts, e.Updates, e.Deletes)
			}
		}
	}()

	return migrator.Run(ctx)
}
