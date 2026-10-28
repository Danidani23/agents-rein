package cli

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/adapters/execute"
	"github.com/Danidani23/agents-rein/internal/app/0_gateway/web"
	"github.com/Danidani23/agents-rein/internal/version"
)

func newUICommand(environment *commandEnvironment) *cobra.Command {
	var noBrowser bool
	command := &cobra.Command{
		Use:   "ui",
		Short: "Show what rein knows on a page in your browser, served from this machine",
		Long: "ui serves pages from this machine and opens them in your browser: what rein status shows of\n" +
			"the machine, and each project with what it resolves and why, the files sync wrote, and Sync\n" +
			"and Reset project, which run as the commands do, one at a time. It listens on 127.0.0.1 only,\n" +
			"on a free port. The address it prints carries a token made for this run, which the first\n" +
			"visit trades for a cookie; a request without it, or naming another host, is refused. It runs\n" +
			"until Ctrl-C.",
		Example: "  rein ui\n  rein ui --no-browser",
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			orchestrator, err := environment.orchestrate()
			if err != nil {
				return err
			}
			page, err := web.Open(orchestrator, version.String(), web.Interrupt{Stop: execute.Interrupt, Running: execute.Running})
			if err != nil {
				return err
			}
			fmt.Fprintf(environment.out, "rein ui at %s\n%s\n", page.Address(), environment.style().Dim("Ctrl-C stops it"))
			if !noBrowser {
				go openInBrowser(environment, page.Address())
			}
			return page.Serve(command.Context())
		},
	}
	command.Flags().BoolVar(&noBrowser, "no-browser", false, "print the address without opening a browser")
	return command
}

// openInBrowser opens address with the operating system's own command. A
// failure is a warning: the printed address still works.
func openInBrowser(environment *commandEnvironment, address string) {
	program, arguments := browserCommand(runtime.GOOS, address)
	_, err := execute.Run("", program, arguments...)
	if err != nil {
		fmt.Fprintf(environment.errOut, "warning: the browser did not open: %v; open the address above\n", err)
	}
}

// browserCommand is the program that opens address in the default browser
// of operatingSystem, with its arguments.
func browserCommand(operatingSystem string, address string) (program string, arguments []string) {
	switch operatingSystem {
	case "darwin":
		return "open", []string{address}
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", address}
	}
	return "xdg-open", []string{address}
}
