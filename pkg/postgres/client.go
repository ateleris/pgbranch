// Package postgres provides PostgreSQL database operations for managing
// database snapshots and template databases.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/le-vlad/pgbranch/pkg/config"
)

// objectInUseSQLState is the SQLSTATE Postgres returns when a DROP DATABASE,
// CREATE DATABASE ... TEMPLATE or ALTER DATABASE ... RENAME cannot proceed
// because other sessions are still connected (e.g. TimescaleDB background
// workers reconnecting right after they are terminated).
const objectInUseSQLState = "55006"

func isObjectInUse(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == objectInUseSQLState
	}
	return false
}

const maxObjectInUseAttempts = 5

// retryOnObjectInUse runs fn, terminating connections to dbName before each
// attempt. If fn fails with SQLSTATE 55006 (object in use) it retries with a
// short backoff, up to maxObjectInUseAttempts times.
func (c *Client) retryOnObjectInUse(ctx context.Context, dbName string, fn func() error) error {
	var err error
	for attempt := 1; attempt <= maxObjectInUseAttempts; attempt++ {
		_ = c.TerminateConnectionsTo(ctx, dbName)

		err = fn()
		if err == nil {
			return nil
		}
		if !isObjectInUse(err) || attempt == maxObjectInUseAttempts {
			return err
		}
		time.Sleep(time.Duration(attempt) * 100 * time.Millisecond)
	}
	return err
}

// Client provides methods for PostgreSQL database operations.
type Client struct {
	Config     *config.Config
	runDump    func(ctx context.Context, args []string, env []string, w io.Writer) error
	runRestore func(ctx context.Context, args []string, env []string, r io.Reader) (string, error)
}

// NewClient creates a new PostgreSQL client with the given configuration.
func NewClient(cfg *config.Config) *Client {
	return &Client{
		Config:     cfg,
		runDump:    defaultRunDump,
		runRestore: defaultRunRestore,
	}
}

func (c *Client) connect(ctx context.Context, dbName string) (*pgx.Conn, error) {
	connStr := c.Config.ConnectionURLForDB(dbName)
	conn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database %s: %w", dbName, err)
	}
	return conn, nil
}

func (c *Client) connectAdmin(ctx context.Context) (*pgx.Conn, error) {
	return c.connect(ctx, "postgres")
}

// DatabaseExists checks if the configured database exists.
func (c *Client) DatabaseExists(ctx context.Context) (bool, error) {
	conn, err := c.connectAdmin(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check database existence: %w", err)
	}
	defer conn.Close(ctx)

	var exists bool
	err = conn.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)",
		c.Config.Database,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check database existence: %w", err)
	}

	return exists, nil
}

// CreateDatabase creates the configured database.
func (c *Client) CreateDatabase(ctx context.Context) error {
	conn, err := c.connectAdmin(ctx)
	if err != nil {
		return fmt.Errorf("failed to create database: %w", err)
	}
	defer conn.Close(ctx)

	_, err = conn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", pgx.Identifier{c.Config.Database}.Sanitize()))
	if err != nil {
		return fmt.Errorf("failed to create database: %w", err)
	}
	return nil
}

// DropDatabase drops the configured database if it exists.
func (c *Client) DropDatabase(ctx context.Context) error {
	conn, err := c.connectAdmin(ctx)
	if err != nil {
		return fmt.Errorf("failed to drop database: %w", err)
	}
	defer conn.Close(ctx)

	_, err = conn.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", pgx.Identifier{c.Config.Database}.Sanitize()))
	if err != nil {
		return fmt.Errorf("failed to drop database: %w", err)
	}
	return nil
}

// TerminateConnections terminates all connections to the configured database.
func (c *Client) TerminateConnections(ctx context.Context) error {
	return c.TerminateConnectionsTo(ctx, c.Config.Database)
}

// TestConnection verifies that a connection can be established to PostgreSQL.
func (c *Client) TestConnection(ctx context.Context) error {
	conn, err := c.connectAdmin(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}
	defer conn.Close(ctx)

	err = conn.Ping(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}
	return nil
}

// CreateDatabaseFromTemplate creates a new database using the specified
// template database. It retries when the template database is briefly busy
// (e.g. TimescaleDB background workers reconnecting after being terminated).
func (c *Client) CreateDatabaseFromTemplate(ctx context.Context, templateDB, newDB string) error {
	query := fmt.Sprintf("CREATE DATABASE %s TEMPLATE %s",
		pgx.Identifier{newDB}.Sanitize(),
		pgx.Identifier{templateDB}.Sanitize(),
	)

	err := c.retryOnObjectInUse(ctx, templateDB, func() error {
		conn, err := c.connectAdmin(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close(ctx) }()

		_, err = conn.Exec(ctx, query)
		return err
	})
	if err != nil {
		return fmt.Errorf("failed to create database from template: %w", err)
	}
	return nil
}

// TerminateConnectionsTo terminates all connections to the specified database.
func (c *Client) TerminateConnectionsTo(ctx context.Context, dbName string) error {
	conn, err := c.connectAdmin(ctx)
	if err != nil {
		return nil
	}
	defer conn.Close(ctx)

	_, _ = conn.Exec(ctx, `
		SELECT pg_terminate_backend(pid)
		FROM pg_stat_activity
		WHERE datname = $1 AND pid <> pg_backend_pid()
	`, dbName)

	return nil
}

// DropDatabaseByName drops the specified database if it exists. It retries
// when the database is briefly busy (e.g. TimescaleDB background workers
// reconnecting after being terminated).
func (c *Client) DropDatabaseByName(ctx context.Context, dbName string) error {
	query := fmt.Sprintf("DROP DATABASE IF EXISTS %s", pgx.Identifier{dbName}.Sanitize())

	err := c.retryOnObjectInUse(ctx, dbName, func() error {
		conn, err := c.connectAdmin(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close(ctx) }()

		_, err = conn.Exec(ctx, query)
		return err
	})
	if err != nil {
		return fmt.Errorf("failed to drop database: %w", err)
	}
	return nil
}

// RenameDatabase renames database from to to. It terminates connections to
// from first and retries on SQLSTATE 55006 (object in use), since
// TimescaleDB background workers may reconnect right after termination.
func (c *Client) RenameDatabase(ctx context.Context, from, to string) error {
	query := fmt.Sprintf("ALTER DATABASE %s RENAME TO %s",
		pgx.Identifier{from}.Sanitize(),
		pgx.Identifier{to}.Sanitize(),
	)

	err := c.retryOnObjectInUse(ctx, from, func() error {
		conn, err := c.connectAdmin(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close(ctx) }()

		_, err = conn.Exec(ctx, query)
		return err
	})
	if err != nil {
		return fmt.Errorf("failed to rename database %s to %s: %w", from, to, err)
	}
	return nil
}

// Exists checks whether the named database exists.
func (c *Client) Exists(ctx context.Context, db string) (bool, error) {
	conn, err := c.connectAdmin(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check database existence: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	var exists bool
	err = conn.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)",
		db,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check database existence: %w", err)
	}
	return exists, nil
}

// Size returns the on-disk size of the named database in bytes.
func (c *Client) Size(ctx context.Context, db string) (int64, error) {
	conn, err := c.connectAdmin(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get database size: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	var size int64
	err = conn.QueryRow(ctx, "SELECT pg_database_size($1)", db).Scan(&size)
	if err != nil {
		return 0, fmt.Errorf("failed to get database size: %w", err)
	}
	return size, nil
}

// TableCount returns the number of user tables in db (excluding the
// pg_catalog and information_schema system schemas).
func (c *Client) TableCount(ctx context.Context, db string) (int, error) {
	conn, err := c.connect(ctx, db)
	if err != nil {
		return 0, fmt.Errorf("failed to count tables: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	var count int
	err = conn.QueryRow(ctx,
		`SELECT COUNT(*) FROM information_schema.tables
		 WHERE table_schema NOT IN ('pg_catalog', 'information_schema')`,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count tables: %w", err)
	}
	return count, nil
}
