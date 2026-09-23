package postgres

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// quoteLiteral quotes s as a Postgres string literal (as opposed to
// pgx.Identifier{...}.Sanitize(), which quotes an identifier).
func quoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// checkDumpToolsAvailable verifies pg_dump and pg_restore are on PATH,
// returning an error with an install hint if not.
func checkDumpToolsAvailable() error {
	for _, bin := range []string{"pg_dump", "pg_restore"} {
		if _, err := exec.LookPath(bin); err != nil {
			return fmt.Errorf(
				"%s not found in PATH: install the PostgreSQL client tools "+
					"(e.g. `apt install postgresql-client`, `brew install libpq`) "+
					"to use the dump clone strategy",
				bin,
			)
		}
	}
	return nil
}

// execOnDB runs a single statement against db using a dedicated connection.
func (c *Client) execOnDB(ctx context.Context, db, sql string) error {
	conn, err := c.connect(ctx, db)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(ctx) }()

	_, err = conn.Exec(ctx, sql)
	return err
}

// cloneDump clones src into dst by streaming pg_dump into pg_restore. dst is
// created empty first. If src has the timescaledb extension installed, dst
// goes through timescaledb_pre_restore/timescaledb_post_restore around the
// restore, matching TimescaleDB's documented pg_dump/pg_restore workflow:
// CREATE EXTENSION, pre_restore (which sets the timescaledb.restoring GUC at
// the database level via ALTER DATABASE, so it is visible to the separate
// session pg_restore connects with), restore, then always post_restore.
func (c *Client) cloneDump(ctx context.Context, src, dst string) error {
	if err := checkDumpToolsAvailable(); err != nil {
		return err
	}

	if err := c.CreateEmptyDatabase(ctx, dst); err != nil {
		return fmt.Errorf("failed to create destination database: %w", err)
	}

	hasTimescale, err := c.HasExtension(ctx, src, "timescaledb")
	if err != nil {
		return fmt.Errorf("failed to check source extensions: %w", err)
	}

	if hasTimescale {
		version, err := c.ExtensionVersion(ctx, src, "timescaledb")
		if err != nil {
			return fmt.Errorf("failed to determine timescaledb version: %w", err)
		}

		defaultVersion, err := c.ExtensionDefaultVersion(ctx, src, "timescaledb")
		if err != nil {
			return fmt.Errorf("failed to determine the server's default timescaledb version: %w", err)
		}

		// CREATE EXTENSION IF NOT EXISTS without VERSION installs the
		// server's current default version, which can diverge from the
		// source's installed version after a timescaledb package upgrade
		// that hasn't been followed by `ALTER EXTENSION timescaledb
		// UPDATE` on src. Restoring src's data (dumped from its actual,
		// stale-versioned catalog) into a destination created at a
		// *different* version is not safe: verified against a real
		// container, TimescaleDB's own restore hooks and/or the dumped
		// data's column layout do not tolerate the mismatch, so fail
		// loudly up front instead of restoring inconsistent or incomplete
		// data.
		if version != defaultVersion {
			return fmt.Errorf(
				"source database %q has timescaledb extension version %s installed, but this server's "+
					"current default version is %s; run `ALTER EXTENSION timescaledb UPDATE` on it before "+
					"cloning (a stale extension version cannot be safely dumped and restored)",
				src, version, defaultVersion,
			)
		}

		createSQL := fmt.Sprintf("CREATE EXTENSION IF NOT EXISTS timescaledb VERSION %s", quoteLiteral(version))
		if err := c.execOnDB(ctx, dst, createSQL); err != nil {
			return fmt.Errorf("failed to create timescaledb extension: %w", err)
		}
		if err := c.execOnDB(ctx, dst, "SELECT timescaledb_pre_restore()"); err != nil {
			return fmt.Errorf("failed to run timescaledb_pre_restore: %w", err)
		}
	}

	restoreErr := c.streamDumpRestore(ctx, src, dst)

	if hasTimescale {
		// timescaledb_post_restore must run whether or not the restore
		// itself succeeded, otherwise dst is left with
		// timescaledb.restoring permanently set. Its error is surfaced
		// when the restore otherwise succeeded, since leaving that GUC set
		// must not be silently reported as a good clone; if the restore
		// itself already failed, that is the more useful error to return.
		if postErr := c.execOnDB(ctx, dst, "SELECT timescaledb_post_restore()"); postErr != nil && restoreErr == nil {
			restoreErr = fmt.Errorf("failed to run timescaledb_post_restore: %w", postErr)
		}
	}

	if restoreErr != nil {
		return restoreErr
	}

	srcCount, err := c.TableCount(ctx, src)
	if err != nil {
		return fmt.Errorf("failed to count source tables: %w", err)
	}
	dstCount, err := c.TableCount(ctx, dst)
	if err != nil {
		return fmt.Errorf("failed to count destination tables: %w", err)
	}
	if dstCount < srcCount {
		return fmt.Errorf(
			"restore verification failed: destination has %d tables, source has %d",
			dstCount, srcCount,
		)
	}

	return nil
}

// streamDumpRestore pipes pg_dump of src directly into pg_restore against
// dst, without buffering the whole dump in memory.
func (c *Client) streamDumpRestore(ctx context.Context, src, dst string) error {
	pr, pw := io.Pipe()

	dumpErrCh := make(chan error, 1)
	go func() {
		err := c.DumpDatabase(ctx, src, pw, nil)
		if err != nil {
			_ = pw.CloseWithError(err)
		} else {
			_ = pw.Close()
		}
		dumpErrCh <- err
	}()

	restoreErr := c.RestoreDatabase(ctx, dst, pr)
	dumpErr := <-dumpErrCh

	if dumpErr != nil {
		return fmt.Errorf("pg_dump failed: %w", dumpErr)
	}
	if restoreErr != nil {
		return fmt.Errorf("pg_restore failed: %w", restoreErr)
	}
	return nil
}
