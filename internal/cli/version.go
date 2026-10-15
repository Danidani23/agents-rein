package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Danidani23/reins/internal/version"
)

func newVersionCommand(env *environment) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the reins version",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			fmt.Fprintf(env.out, "reins %s\n", version.String())
			return nil
		},
	}
}
