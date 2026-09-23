package postgres

import (
	"context"
	"fmt"
)

// CloneDatabase copies src into dst using the given strategy (StrategyAuto is
// resolved against src). dst must not already exist. If cloning fails, dst is
// dropped so no partial database is left behind.
func (c *Client) CloneDatabase(ctx context.Context, src, dst string, s Strategy) error {
	exists, err := c.Exists(ctx, dst)
	if err != nil {
		return fmt.Errorf("failed to clone database: %w", err)
	}
	if exists {
		return fmt.Errorf("failed to clone database: destination %q already exists", dst)
	}

	resolved, err := c.ResolveStrategy(ctx, src, s)
	if err != nil {
		return fmt.Errorf("failed to clone database: %w", err)
	}

	switch resolved {
	case StrategyTemplate:
		err = c.cloneTemplate(ctx, src, dst)
	case StrategyDump:
		err = c.cloneDump(ctx, src, dst)
	default:
		err = fmt.Errorf("unknown clone strategy %q", resolved)
	}

	if err != nil {
		_ = c.DropDatabaseByName(ctx, dst)
		return fmt.Errorf("failed to clone database %s into %s: %w", src, dst, err)
	}
	return nil
}

// cloneTemplate clones src into dst with CREATE DATABASE ... TEMPLATE ...,
// retrying if src is briefly busy.
func (c *Client) cloneTemplate(ctx context.Context, src, dst string) error {
	return c.CreateDatabaseFromTemplate(ctx, src, dst)
}
