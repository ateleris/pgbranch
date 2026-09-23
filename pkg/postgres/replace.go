package postgres

import (
	"context"
	"fmt"
)

// tmpSuffix names the scratch database used by ReplaceDatabase while a clone
// is being built, so a failed clone never touches the target database.
const tmpSuffix = "__pgbranch_tmp"

// ReplaceDatabase safely replaces target with a fresh clone of src. It
// clones src into a scratch database (target+"__pgbranch_tmp", dropping any
// stale leftover from a previous failed run first), and only once that
// succeeds does it terminate connections to and drop target, then rename the
// scratch database into place. If cloning fails, target is left untouched
// and the scratch database is dropped.
func (c *Client) ReplaceDatabase(ctx context.Context, src, target string, s Strategy) error {
	tmp := target + tmpSuffix

	if err := c.DropDatabaseByName(ctx, tmp); err != nil {
		return fmt.Errorf("failed to drop stale scratch database %s: %w", tmp, err)
	}

	if err := c.CloneDatabase(ctx, src, tmp, s); err != nil {
		_ = c.DropDatabaseByName(ctx, tmp)
		return fmt.Errorf("failed to clone %s: %w", src, err)
	}

	if err := c.DropDatabaseByName(ctx, target); err != nil {
		_ = c.DropDatabaseByName(ctx, tmp)
		return fmt.Errorf("failed to drop target database %s: %w", target, err)
	}

	if err := c.RenameDatabase(ctx, tmp, target); err != nil {
		return fmt.Errorf("failed to rename %s to %s: %w", tmp, target, err)
	}

	return nil
}
