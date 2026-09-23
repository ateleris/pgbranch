package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolvedVersionUsesExplicitBuildInfo(t *testing.T) {
	original := buildInfo
	defer func() { buildInfo = original }()

	buildInfo = BuildInfo{Version: "v1.2.3", Commit: "abc123", Date: "2026-09-23"}

	assert.Equal(t, "v1.2.3", resolvedVersion())
}

func TestResolvedVersionFallsBackWhenDev(t *testing.T) {
	original := buildInfo
	defer func() { buildInfo = original }()

	buildInfo = BuildInfo{Version: "dev", Commit: "unknown", Date: "unknown"}

	// debug.ReadBuildInfo() reports "(devel)" for a binary built directly
	// from source without a version tag (as `go test` does here), in which
	// case resolvedVersion falls back to returning "dev" unchanged.
	assert.Equal(t, "dev", resolvedVersion())
}
