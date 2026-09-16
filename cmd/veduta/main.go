// Command veduta is the Veduta tool: asset cooking, headless render and simulation through
// the game binary, inspection reports, diffs and queries, tests, fuzzing, releases, and
// the MCP server for AI agents. Run `veduta help` for the list of commands.
package main

import (
	"os"

	"github.com/riftbane/veduta/v2/internal/cli"
)

// Set by the release build: -ldflags "-X main.version=vX.Y.Z -X main.commit=… -X main.date=…".
var (
	version = "dev"
	commit  = ""
	date    = ""
)

func main() {
	os.Exit(cli.Main(os.Args[1:], cli.Env{Version: version, Commit: commit, Date: date}))
}
