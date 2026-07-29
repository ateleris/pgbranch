package core

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBranchErrorMessages(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		sentinel error
		want     string
	}{
		{
			name:     "not found",
			err:      branchNotFound("feature-x"),
			sentinel: ErrBranchNotFound,
			want:     "branch 'feature-x' does not exist",
		},
		{
			name:     "already exists",
			err:      branchExists("feature-x"),
			sentinel: ErrBranchExists,
			want:     "branch 'feature-x' already exists",
		},
		{
			name:     "current branch",
			err:      currentBranchError("main"),
			sentinel: ErrCurrentBranch,
			want:     "cannot delete the current branch 'main'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.err.Error())
			assert.ErrorIs(t, tt.err, tt.sentinel)
		})
	}
}

// The message must not name a command line flag: pkg/core is used by embedders
// that have no --force flag. The CLI adds that guidance itself.
func TestBranchErrorMentionsNoFlags(t *testing.T) {
	assert.NotContains(t, currentBranchError("main").Error(), "--force")
}

func TestBranchErrorCarriesName(t *testing.T) {
	err := branchNotFound("feature-x")

	var branchErr *BranchError
	require.ErrorAs(t, err, &branchErr)
	assert.Equal(t, "feature-x", branchErr.Name)
	assert.Equal(t, ErrBranchNotFound, branchErr.Err)
}

func TestBranchErrorsAreDistinguishable(t *testing.T) {
	notFound := branchNotFound("x")

	assert.ErrorIs(t, notFound, ErrBranchNotFound)
	assert.NotErrorIs(t, notFound, ErrBranchExists)
	assert.NotErrorIs(t, notFound, ErrCurrentBranch)
	assert.False(t, errors.Is(notFound, errors.New("branch does not exist")),
		"must not match an unrelated error with the same text")
}
