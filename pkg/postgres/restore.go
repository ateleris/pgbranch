package postgres

import (
	"context"
	"fmt"
)

// RestoreFromSnapshot replaces the configured database with a copy of the
// given snapshot database.
func (c *Client) RestoreFromSnapshot(ctx context.Context, snapshotDBName string) error {
	c.TerminateConnections(ctx)

	if err := c.DropDatabase(ctx); err != nil {
		return fmt.Errorf("failed to drop database: %w", err)
	}

	if err := c.CreateDatabaseFromTemplate(ctx, snapshotDBName, c.Config.Database); err != nil {
		return fmt.Errorf("failed to create database from snapshot: %w", err)
	}

	return nil
}

// DeleteSnapshot drops the given snapshot database.
func (c *Client) DeleteSnapshot(ctx context.Context, snapshotDBName string) error {
	return c.DropDatabaseByName(ctx, snapshotDBName)
}
