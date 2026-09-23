package main

import "github.com/le-vlad/pgbranch/internal/cli"

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	cli.Execute(cli.BuildInfo{Version: version, Commit: commit, Date: date})
}
