package storage

import (
	"crypto/sha1" //nolint:gosec // used only for a short non-cryptographic name hash
	"encoding/hex"
	"fmt"
	"strings"
)

// SanitizeBranch normalizes a git branch name for use in a database name:
// it lowercases the input, replaces every rune outside [a-z0-9_] with an
// underscore (a multi-byte rune counts as a single replacement), and
// collapses runs of underscores into one.
func SanitizeBranch(branch string) string {
	lower := strings.ToLower(branch)

	var b strings.Builder
	b.Grow(len(lower))
	prevUnderscore := false
	for _, r := range lower {
		isValid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		out := r
		if !isValid {
			out = '_'
		}

		if out == '_' {
			if prevUnderscore {
				continue
			}
			prevUnderscore = true
		} else {
			prevUnderscore = false
		}
		b.WriteRune(out)
	}

	return b.String()
}

// SnapshotDBName generates a database name for a snapshot.
// Format: {originalDB}_pgbranch_{sanitized branchName}
//
// If the resulting name exceeds 63 bytes (the Postgres identifier limit),
// it is truncated to 63 bytes total, where the last 7 bytes are an
// underscore followed by the first 6 hex characters of the sha1 hash of
// the original, unsanitized branch name. This keeps names deterministic
// and disambiguates long branch names that share a common prefix.
func SnapshotDBName(originalDB, branchName string) string {
	sanitized := SanitizeBranch(branchName)
	name := fmt.Sprintf("%s_pgbranch_%s", originalDB, sanitized)

	const maxLen = 63
	if len(name) <= maxLen {
		return name
	}

	sum := sha1.Sum([]byte(branchName)) //nolint:gosec // non-cryptographic use
	hash := hex.EncodeToString(sum[:])[:6]
	suffix := "_" + hash

	return name[:maxLen-len(suffix)] + suffix
}
