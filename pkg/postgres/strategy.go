package postgres

import (
	"context"
	"fmt"
)

// Strategy selects how a database is cloned into a snapshot (or vice versa).
type Strategy string

const (
	// StrategyAuto picks dump if the source database has the timescaledb
	// extension installed, template otherwise.
	StrategyAuto Strategy = "auto"
	// StrategyTemplate clones via `CREATE DATABASE ... TEMPLATE ...`.
	StrategyTemplate Strategy = "template"
	// StrategyDump clones via a streamed pg_dump | pg_restore.
	StrategyDump Strategy = "dump"
)

// ParseStrategy parses a strategy string from config/CLI input. An empty
// string maps to StrategyAuto.
func ParseStrategy(s string) (Strategy, error) {
	switch Strategy(s) {
	case "":
		return StrategyAuto, nil
	case StrategyAuto, StrategyTemplate, StrategyDump:
		return Strategy(s), nil
	default:
		return "", fmt.Errorf("invalid clone strategy %q (must be one of: auto, template, dump)", s)
	}
}

// HasExtension reports whether the given extension is installed in db.
func (c *Client) HasExtension(ctx context.Context, db, ext string) (bool, error) {
	conn, err := c.connect(ctx, db)
	if err != nil {
		return false, fmt.Errorf("failed to check extension %s: %w", ext, err)
	}
	defer func() { _ = conn.Close(ctx) }()

	var exists bool
	err = conn.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname = $1)",
		ext,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check extension %s: %w", ext, err)
	}
	return exists, nil
}

// ExtensionVersion returns the installed version string of ext in db. It
// returns an error if the extension is not installed.
func (c *Client) ExtensionVersion(ctx context.Context, db, ext string) (string, error) {
	conn, err := c.connect(ctx, db)
	if err != nil {
		return "", fmt.Errorf("failed to get extension %s version: %w", ext, err)
	}
	defer func() { _ = conn.Close(ctx) }()

	var version string
	err = conn.QueryRow(ctx,
		"SELECT extversion FROM pg_extension WHERE extname = $1",
		ext,
	).Scan(&version)
	if err != nil {
		return "", fmt.Errorf("failed to get extension %s version: %w", ext, err)
	}
	return version, nil
}

// ExtensionDefaultVersion returns the version that `CREATE EXTENSION ext`
// installs by default in db when no VERSION clause is given -- the version
// matching the currently loaded extension library.
func (c *Client) ExtensionDefaultVersion(ctx context.Context, db, ext string) (string, error) {
	conn, err := c.connect(ctx, db)
	if err != nil {
		return "", fmt.Errorf("failed to get default version for extension %s: %w", ext, err)
	}
	defer func() { _ = conn.Close(ctx) }()

	var version string
	err = conn.QueryRow(ctx,
		"SELECT default_version FROM pg_available_extensions WHERE name = $1",
		ext,
	).Scan(&version)
	if err != nil {
		return "", fmt.Errorf("failed to get default version for extension %s: %w", ext, err)
	}
	return version, nil
}

// ResolveStrategy resolves StrategyAuto against db (dump if db has the
// timescaledb extension installed, template otherwise). Any other strategy
// is returned unchanged.
func (c *Client) ResolveStrategy(ctx context.Context, db string, s Strategy) (Strategy, error) {
	if s != StrategyAuto {
		return s, nil
	}

	hasTimescale, err := c.HasExtension(ctx, db, "timescaledb")
	if err != nil {
		return "", fmt.Errorf("failed to resolve clone strategy: %w", err)
	}
	if hasTimescale {
		return StrategyDump, nil
	}
	return StrategyTemplate, nil
}
