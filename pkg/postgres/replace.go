package postgres

import (
	"context"
	"crypto/sha1" //nolint:gosec // used only for a short non-cryptographic name hash
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

// maxIdentifierLen is the Postgres identifier length limit (NAMEDATALEN-1).
// Names longer than this are silently truncated by the server regardless of
// quoting, so pgbranch rejects them outright instead of risking two distinct
// names colliding after truncation.
const maxIdentifierLen = 63

// duplicateDatabaseSQLState is the SQLSTATE Postgres returns when a
// CREATE/ALTER DATABASE ... RENAME targets a name that already exists --
// e.g. something else recreated the target database in the brief window
// between PreparedReplace.Commit dropping it and renaming the scratch
// database into its place.
const duplicateDatabaseSQLState = "42P04"

// ScratchDBName returns the scratch database name PrepareReplace uses while
// a clone is being built for target. It has a fixed length well under the
// Postgres identifier limit and is derived from a hash of target, so it
// never collides with target regardless of target's length (unlike a naive
// target+suffix concatenation, which Postgres would truncate to exactly
// target's own name for a 63-byte target).
func ScratchDBName(target string) string {
	sum := sha1.Sum([]byte(target)) //nolint:gosec // non-cryptographic use
	return "pgbranch_tmp_" + hex.EncodeToString(sum[:])[:8]
}

func checkIdentifierLen(name string) error {
	if len(name) > maxIdentifierLen {
		return fmt.Errorf("database identifier %q is %d bytes, exceeding the Postgres limit of %d", name, len(name), maxIdentifierLen)
	}
	return nil
}

// PreparedReplace holds a scratch clone of a source database, ready to be
// swapped into place over a target database. Exactly one of Commit or
// Abort must be called on it.
//
// Splitting a replace into Prepare (clone src into scratch -- safe,
// non-destructive) and Commit (drop target, rename scratch into place --
// destructive) lets a caller replacing several databases as one logical
// operation (e.g. every database on a branch checkout) build every scratch
// clone first, so a failure cloning any one of them never touches any
// target database at all, instead of leaving some databases already
// swapped to the new branch and others not.
type PreparedReplace struct {
	client  *Client
	target  string
	scratch string
}

// PrepareReplace clones src into a scratch database for target (dropping
// any stale leftover scratch database from a previous failed run first).
// It does not touch target. If cloning fails, the scratch database is
// dropped and an error is returned; there is nothing to Abort in that case.
func (c *Client) PrepareReplace(ctx context.Context, src, target string, s Strategy) (*PreparedReplace, error) {
	if err := checkIdentifierLen(target); err != nil {
		return nil, err
	}

	scratch := ScratchDBName(target)

	if err := c.DropDatabaseByName(ctx, scratch); err != nil {
		return nil, fmt.Errorf("failed to drop stale scratch database %s: %w", scratch, err)
	}

	if err := c.CloneDatabase(ctx, src, scratch, s); err != nil {
		_ = c.DropDatabaseByName(ctx, scratch)
		return nil, fmt.Errorf("failed to clone %s: %w", src, err)
	}

	return &PreparedReplace{client: c, target: target, scratch: scratch}, nil
}

// Commit drops the target database and renames the prepared scratch
// database into its place. After Commit is called, whether it succeeds or
// fails, p must not be used again.
//
// A failure here (as opposed to during Prepare) means target has already
// been dropped and the swap did not complete, so the caller must treat its
// state as inconsistent rather than assuming target still holds its
// previous data.
func (p *PreparedReplace) Commit(ctx context.Context) error {
	if err := p.client.DropDatabaseByName(ctx, p.target); err != nil {
		_ = p.client.DropDatabaseByName(ctx, p.scratch)
		return fmt.Errorf("failed to drop target database %s: %w", p.target, err)
	}

	if err := p.client.RenameDatabase(ctx, p.scratch, p.target); err != nil {
		if isDuplicateDatabase(err) {
			return fmt.Errorf(
				"failed to rename %s to %s: a database named %q already exists "+
					"(something else must have recreated it): %w",
				p.scratch, p.target, p.target, err,
			)
		}
		return fmt.Errorf("failed to rename %s to %s: %w", p.scratch, p.target, err)
	}

	return nil
}

// Abort discards the prepared scratch database without touching target.
// Safe to call even if the scratch database no longer exists.
func (p *PreparedReplace) Abort(ctx context.Context) {
	_ = p.client.DropDatabaseByName(ctx, p.scratch)
}

func isDuplicateDatabase(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == duplicateDatabaseSQLState
	}
	return false
}

// ReplaceDatabase safely replaces target with a fresh clone of src: it is
// PrepareReplace immediately followed by Commit. If cloning fails, target
// is left untouched. See PrepareReplace/PreparedReplace.Commit for
// replacing several databases as one atomic-ish unit.
func (c *Client) ReplaceDatabase(ctx context.Context, src, target string, s Strategy) error {
	p, err := c.PrepareReplace(ctx, src, target, s)
	if err != nil {
		return err
	}
	return p.Commit(ctx)
}
