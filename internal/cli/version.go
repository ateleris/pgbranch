package cli

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// BuildInfo carries the version metadata goreleaser embeds via -ldflags into
// cmd/pgbranch/main.go's package-level version/commit/date vars.
type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

var buildInfo = BuildInfo{Version: "dev", Commit: "unknown", Date: "unknown"}

// resolvedVersion returns buildInfo.Version, falling back to the main
// module's version reported by debug.ReadBuildInfo() when it's "dev" (i.e.
// not set via -ldflags). This makes `go install ...@vX` builds report vX
// instead of "dev".
func resolvedVersion() string {
	if buildInfo.Version != "dev" && buildInfo.Version != "" {
		return buildInfo.Version
	}

	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}

	return buildInfo.Version
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the pgbranch version",
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "pgbranch %s (commit %s, built %s)\n",
			resolvedVersion(), buildInfo.Commit, buildInfo.Date)
		return err
	},
}
