package storage

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSnapshotDBName(t *testing.T) {
	tests := []struct {
		name       string
		originalDB string
		branchName string
		expected   string
	}{
		{"simple", "mydb", "main", "mydb_pgbranch_main"},
		{"hyphen", "mydb", "feature-1", "mydb_pgbranch_feature_1"},
		{"slash", "mydb", "feature/login", "mydb_pgbranch_feature_login"},
		{"dots", "mydb", "release.1.0", "mydb_pgbranch_release_1_0"},
		{"hyphen2", "testdb", "my-branch", "testdb_pgbranch_my_branch"},
		{"uppercase", "mydb", "Main", "mydb_pgbranch_main"},
		{"runs collapse", "mydb", "foo---bar", "mydb_pgbranch_foo_bar"},
		{"unicode", "mydb", "Héllo/Wörld", "mydb_pgbranch_h_llo_w_rld"},
		{
			"exactly 63 bytes, no truncation",
			"mydb",
			strings.Repeat("a", 49),
			"mydb_pgbranch_" + strings.Repeat("a", 49),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SnapshotDBName(tt.originalDB, tt.branchName)
			assert.Equal(t, tt.expected, result)
			assert.LessOrEqual(t, len(result), 63)
		})
	}
}

func TestSnapshotDBName_TruncatesOverLongNames(t *testing.T) {
	branch := strings.Repeat("a", 50) // sanitized name is 64 bytes -> must truncate
	result := SnapshotDBName("mydb", branch)

	assert.Len(t, result, 63)
	assert.True(t, strings.HasSuffix(result, "_6c1773"), "expected hash suffix, got %s", result)
}

func TestSnapshotDBName_LongNamesWithSharedPrefixAreDistinct(t *testing.T) {
	b1 := "this-is-a-really-long-branch-name-that-will-definitely-exceed-limit"
	b2 := "this-is-a-really-long-branch-name-that-will-definitely-exceed-limit-2"

	n1 := SnapshotDBName("mydb", b1)
	n2 := SnapshotDBName("mydb", b2)

	assert.Len(t, n1, 63)
	assert.Len(t, n2, 63)
	assert.NotEqual(t, n1, n2)
}

func TestSanitizeBranch(t *testing.T) {
	tests := []struct {
		branch   string
		expected string
	}{
		{"main", "main"},
		{"Main", "main"},
		{"feature/login", "feature_login"},
		{"release.1.0", "release_1_0"},
		{"foo---bar", "foo_bar"},
		{"Héllo/Wörld", "h_llo_w_rld"},
		{"__leading__", "_leading_"}, // literal underscores also collapse as a run
	}

	for _, tt := range tests {
		t.Run(tt.branch, func(t *testing.T) {
			assert.Equal(t, tt.expected, SanitizeBranch(tt.branch))
		})
	}
}
