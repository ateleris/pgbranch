package cli

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/le-vlad/pgbranch/pkg/config"
)

// pkg/config reports "not initialized in <dir>", which is right for a library
// but unhelpful on a command line. The CLI restores the actionable wording.
func TestNotInitializedHintNamesTheCommand(t *testing.T) {
	err := notInitializedHint(fmt.Errorf("%w in /some/dir", config.ErrNotInitialized))

	assert.EqualError(t, err, "pgbranch not initialized. Run 'pgbranch init' first")
}

func TestNotInitializedHintPassesOtherErrorsThrough(t *testing.T) {
	original := errors.New("permission denied")

	assert.Same(t, original, notInitializedHint(original))
}

func TestNotInitializedHintPassesNilThrough(t *testing.T) {
	assert.NoError(t, notInitializedHint(nil))
}
