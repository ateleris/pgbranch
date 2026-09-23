package cli

import "testing"

func TestInitialBranches(t *testing.T) {
	tests := []struct {
		name         string
		baseline     string
		gitBranch    string
		wantToCreate []string
		wantCurrent  string
	}{
		{
			name:         "not a git repo or detached HEAD",
			baseline:     "main",
			gitBranch:    "",
			wantToCreate: []string{"main"},
			wantCurrent:  "main",
		},
		{
			name:         "git branch matches baseline",
			baseline:     "main",
			gitBranch:    "main",
			wantToCreate: []string{"main"},
			wantCurrent:  "main",
		},
		{
			name:         "git branch differs from baseline",
			baseline:     "main",
			gitBranch:    "develop",
			wantToCreate: []string{"main", "develop"},
			wantCurrent:  "develop",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toCreate, current := initialBranches(tt.baseline, tt.gitBranch)

			if len(toCreate) != len(tt.wantToCreate) {
				t.Fatalf("toCreate = %v, want %v", toCreate, tt.wantToCreate)
			}
			for i := range toCreate {
				if toCreate[i] != tt.wantToCreate[i] {
					t.Fatalf("toCreate = %v, want %v", toCreate, tt.wantToCreate)
				}
			}
			if current != tt.wantCurrent {
				t.Fatalf("current = %q, want %q", current, tt.wantCurrent)
			}
		})
	}
}
