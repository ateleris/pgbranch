package postgres

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// DumpOptions configures pg_dump behavior
type DumpOptions struct {
	SchemaOnly    bool
	DataOnly      bool
	ExcludeTables []string
}

func defaultRunDump(ctx context.Context, args []string, env []string, w io.Writer) error {
	cmd := exec.CommandContext(ctx, "pg_dump", args...)
	cmd.Stdout = w
	cmd.Env = env
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pg_dump failed: %w\nstderr: %s", err, stderr.String())
	}
	return nil
}

func defaultRunRestore(ctx context.Context, args []string, env []string, r io.Reader) (string, error) {
	cmd := exec.CommandContext(ctx, "pg_restore", args...)
	cmd.Stdin = r
	cmd.Env = env
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stderr.String(), err
}

// DumpDatabase creates a pg_dump of the specified database and writes to the provided writer.
// Uses custom format (-Fc) which is compressed and supports parallel restore.
func (c *Client) DumpDatabase(ctx context.Context, dbName string, w io.Writer, opts *DumpOptions) error {
	args := c.buildDumpArgs(dbName, opts)
	return c.runDump(ctx, args, c.buildEnv(), w)
}

func (c *Client) buildDumpArgs(dbName string, opts *DumpOptions) []string {
	args := []string{
		"-h", c.Config.Host,
		"-p", fmt.Sprintf("%d", c.Config.Port),
		"-U", c.Config.User,
		"-Fc",
		"--no-password",
		dbName,
	}

	if opts != nil {
		if opts.SchemaOnly {
			args = append(args, "--schema-only")
		}
		if opts.DataOnly {
			args = append(args, "--data-only")
		}
		for _, table := range opts.ExcludeTables {
			args = append(args, "--exclude-table", table)
		}
	}

	return args
}

// RestoreDatabase restores a pg_dump to the specified database from the provided reader.
// The database must already exist and be empty.
func (c *Client) RestoreDatabase(ctx context.Context, dbName string, r io.Reader) error {
	args := c.buildRestoreArgs(dbName)
	stderrStr, err := c.runRestore(ctx, args, c.buildEnv(), r)
	if err != nil {
		if isCriticalRestoreError(stderrStr) {
			return fmt.Errorf("pg_restore failed: %w\nstderr: %s", err, stderrStr)
		}
	}
	return nil
}

// benignPgRestoreErrors are pg_restore "error:" line substrings known to be
// safe to ignore: pg_restore continues past them and the destination
// database ends up complete anyway.
//
//   - "unrecognized configuration parameter": a dump made by a newer/older
//     Postgres major version can set a GUC (via a plain SET statement) that
//     doesn't exist on the target version. The SET statement is not data;
//     failing it does not affect any table or object being restored.
var benignPgRestoreErrors = []string{
	"unrecognized configuration parameter",
}

// isCriticalRestoreError reports whether pg_restore's stderr indicates a
// failure serious enough that the destination database cannot be trusted.
// A non-zero pg_restore exit is critical by default -- including one with
// no recognizable "pg_restore: error:" line at all (e.g. the process was
// killed, or it never managed to connect), which must not be silently
// treated as success -- unless every such line matches the benign allowlist
// above.
func isCriticalRestoreError(stderr string) bool {
	sawErrorLine := false
	for _, line := range strings.Split(stderr, "\n") {
		if !strings.Contains(line, "pg_restore: error:") {
			continue
		}
		sawErrorLine = true

		benign := false
		for _, pattern := range benignPgRestoreErrors {
			if strings.Contains(line, pattern) {
				benign = true
				break
			}
		}
		if !benign {
			return true
		}
	}
	return !sawErrorLine
}

func (c *Client) buildRestoreArgs(dbName string) []string {
	return []string{
		"-h", c.Config.Host,
		"-p", fmt.Sprintf("%d", c.Config.Port),
		"-U", c.Config.User,
		"-d", dbName,
		"--no-password",
		"--no-owner",
		"--no-privileges",
	}
}

// buildEnv creates environment variables for pg_dump/pg_restore commands
// It inherits the current environment and adds PGPASSWORD if configured
func (c *Client) buildEnv() []string {
	env := os.Environ()
	if c.Config.Password != "" {
		env = append(env, fmt.Sprintf("PGPASSWORD=%s", c.Config.Password))
	}
	return env
}

func (c *Client) DumpSnapshotToWriter(ctx context.Context, snapshotDBName string, w io.Writer) error {
	return c.DumpDatabase(ctx, snapshotDBName, w, nil)
}

func (c *Client) RestoreSnapshotFromReader(ctx context.Context, snapshotDBName string, r io.Reader) error {
	if err := c.CreateEmptyDatabase(ctx, snapshotDBName); err != nil {
		return fmt.Errorf("failed to create database for restore: %w", err)
	}

	if err := c.RestoreDatabase(ctx, snapshotDBName, r); err != nil {
		_ = c.DropDatabaseByName(ctx, snapshotDBName)
		return fmt.Errorf("failed to restore database: %w", err)
	}

	return nil
}

func (c *Client) CreateEmptyDatabase(ctx context.Context, dbName string) error {
	conn, err := c.connectAdmin(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	query := fmt.Sprintf("CREATE DATABASE %s", sanitizeIdentifier(dbName))
	_, err = conn.Exec(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to create database: %w", err)
	}

	return nil
}

func sanitizeIdentifier(name string) string {
	escaped := strings.ReplaceAll(name, `"`, `""`)
	return fmt.Sprintf(`"%s"`, escaped)
}

func GetPgDumpVersion() (string, error) {
	cmd := exec.Command("pg_dump", "--version")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get pg_dump version: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

func GetPgRestoreVersion() (string, error) {
	cmd := exec.Command("pg_restore", "--version")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get pg_restore version: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}
