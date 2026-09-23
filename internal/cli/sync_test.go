package cli

import "testing"

func TestShouldSync(t *testing.T) {
	tests := []struct {
		name string
		in   syncDecisionInput
		want bool
	}{
		{
			name: "manual invocation on a normal branch runs",
			in:   syncDecisionInput{gitBranch: "feature-x"},
			want: true,
		},
		{
			name: "hook file checkout (flag 0) is a no-op",
			in:   syncDecisionInput{hookMode: true, flag: "0", gitBranch: "feature-x"},
			want: false,
		},
		{
			name: "hook branch checkout (flag 1) runs",
			in:   syncDecisionInput{hookMode: true, flag: "1", gitBranch: "feature-x"},
			want: true,
		},
		{
			name: "detached HEAD is a no-op",
			in:   syncDecisionInput{hookMode: true, flag: "1", gitBranch: ""},
			want: false,
		},
		{
			name: "rebase in progress is a no-op",
			in:   syncDecisionInput{hookMode: true, flag: "1", gitBranch: "feature-x", operationInProgress: true},
			want: false,
		},
		{
			name: "linked worktree without follow_worktrees is a no-op",
			in:   syncDecisionInput{hookMode: true, flag: "1", gitBranch: "feature-x", linkedWorktree: true},
			want: false,
		},
		{
			name: "linked worktree with follow_worktrees runs",
			in:   syncDecisionInput{hookMode: true, flag: "1", gitBranch: "feature-x", linkedWorktree: true, followWorktrees: true},
			want: true,
		},
		{
			name: "same commit and already on the matching db branch is a no-op",
			in: syncDecisionInput{
				hookMode:        true,
				flag:            "1",
				prevHEAD:        "abc123",
				newHEAD:         "abc123",
				gitBranch:       "main",
				dbCurrentBranch: "main",
			},
			want: false,
		},
		{
			name: "same commit but a different db branch still runs",
			in: syncDecisionInput{
				hookMode:        true,
				flag:            "1",
				prevHEAD:        "abc123",
				newHEAD:         "abc123",
				gitBranch:       "main",
				dbCurrentBranch: "feature-x",
			},
			want: true,
		},
		{
			name: "different commits still runs even if branch name matches",
			in: syncDecisionInput{
				hookMode:        true,
				flag:            "1",
				prevHEAD:        "abc123",
				newHEAD:         "def456",
				gitBranch:       "main",
				dbCurrentBranch: "main",
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldSync(tt.in); got != tt.want {
				t.Errorf("shouldSync(%+v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
