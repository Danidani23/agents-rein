package cli

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

func newVersionCommand(env *environment) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the reins version",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			version := "unknown"
			info, ok := debug.ReadBuildInfo()
			if ok && info.Main.Version != "" {
				version = info.Main.Version
			}
			fmt.Fprintf(env.out, "reins %s\n", version)
			return nil
		},
	}
}
