package postgres

import "context"

// CreateSnapshot creates a snapshot database from the configured database
// using it as a template.
func (c *Client) CreateSnapshot(ctx context.Context, snapshotDBName string) error {
	return c.CreateDatabaseFromTemplate(ctx, c.Config.Database, snapshotDBName)
}
