package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFormatCount(t *testing.T) {
	assert.Equal(t, "0", formatCount(0))
	assert.Equal(t, "999", formatCount(999))
	assert.Equal(t, "1,000", formatCount(1000))
	assert.Equal(t, "50,432", formatCount(50432))
	assert.Equal(t, "1,234,567", formatCount(1234567))
}
