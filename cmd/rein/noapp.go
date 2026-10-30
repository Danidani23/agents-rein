//go:build !app

package main

import (
	"fmt"
	"io"

	"github.com/Danidani23/agents-rein/internal/app/0_gateway/cli"
)

// appBuild says this rein carries no app: it is built without cgo, and
// the app is a build of its own (D50).
const appBuild = false

// runApp says the app is not in this build.
func runApp(arguments []string, errOut io.Writer) int {
	fmt.Fprintf(errOut, "rein: this rein is built without the app; the app is installed on its own, and rein runs its background process itself without it\n")
	return cli.ExitRefused
}
