// Command reins is the harness distribution tool: a compiler and a linter for
// agent configuration held in an org repository. It never runs as a service,
// never pushes into a repository and never writes a vendor's policy path.
package main

import (
	"os"

	"github.com/Danidani23/reins/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
