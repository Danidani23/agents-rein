package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/version"
)

func newVersionCommand(environment *commandEnvironment) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the rein version",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			fmt.Fprintf(environment.out, "rein %s\n", version.String())
			return nil
		},
	}
}
