package postgres

import (
	"context"
	"crypto/sha1" //nolint:gosec // used only for a short non-cryptographic name hash
	"encoding/hex"
	"fmt"
)

// maxIdentifierLen is the Postgres identifier length limit (NAMEDATALEN-1).
// Names longer than this are silently truncated by the server regardless of
// quoting, so pgbranch rejects them outright instead of risking two distinct
// names colliding after truncation.
const maxIdentifierLen = 63

// scratchDBName returns the scratch database name used by ReplaceDatabase
// while a clone is being built for target. It has a fixed length well under
// the Postgres identifier limit and is derived from a hash of target, so it
// never collides with target regardless of target's length (unlike a naive
// target+suffix concatenation, which Postgres would truncate to exactly
// target's own name for a 63-byte target).
func scratchDBName(target string) string {
	sum := sha1.Sum([]byte(target)) //nolint:gosec // non-cryptographic use
	return "pgbranch_tmp_" + hex.EncodeToString(sum[:])[:8]
}

func checkIdentifierLen(name string) error {
	if len(name) > maxIdentifierLen {
		return fmt.Errorf("database identifier %q is %d bytes, exceeding the Postgres limit of %d", name, len(name), maxIdentifierLen)
	}
	return nil
}

// ReplaceDatabase safely replaces target with a fresh clone of src. It
// clones src into a scratch database (dropping any stale leftover from a
// previous failed run first), and only once that succeeds does it terminate
// connections to and drop target, then rename the scratch database into
// place. If cloning fails, target is left untouched and the scratch
// database is dropped.
func (c *Client) ReplaceDatabase(ctx context.Context, src, target string, s Strategy) error {
	if err := checkIdentifierLen(target); err != nil {
		return err
	}

	tmp := scratchDBName(target)

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
