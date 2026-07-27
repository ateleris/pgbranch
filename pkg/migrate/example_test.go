package migrate_test

import (
	"context"
	"fmt"
	"log"

	"github.com/le-vlad/pgbranch/pkg/migrate"
)

// Running a migration from an application and consuming its progress as
// typed events. Nothing is written to stdout by the library itself.
func ExampleMigrator_Events() {
	cfg, err := migrate.LoadConfig("migrate.yaml")
	if err != nil {
		log.Fatal(err)
	}

	migrator := migrate.NewMigrator(cfg, false, migrate.RunFull)

	go func() {
		for event := range migrator.Events() {
			switch e := event.(type) {
			case migrate.PhaseEvent:
				fmt.Println("phase:", e.Phase)
			case migrate.TableInitEvent:
				fmt.Printf("copying %s (%d rows)\n", e.Table, e.TotalRows)
			case migrate.TableDoneEvent:
				fmt.Println("done:", e.Table)
			case migrate.StreamingEvent:
				fmt.Printf("lsn %s: +%d ~%d -%d\n", e.LSN, e.Inserts, e.Updates, e.Deletes)
			}
		}
	}()

	// Run blocks until the migration finishes or ctx is cancelled. Cancelling
	// stops the WAL streaming phase cleanly.
	if err := migrator.Run(context.Background()); err != nil {
		log.Fatal(err)
	}
}
