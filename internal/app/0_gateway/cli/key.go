package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
)

// keyLimit is the most of standard input read as a key.
const keyLimit = 64 * 1024

// newKeyCommand keeps the key of the organisation's distiller provider in
// the operating system's keychain, so it lies in no shell profile (Q59).
func newKeyCommand(environment *commandEnvironment) *cobra.Command {
	var organization string
	command := &cobra.Command{
		Use:   "key",
		Short: "Keep the key of the organisation's distiller provider in your keychain",
		Long: "key keeps the key of a model provider the organisation's providers.yaml declares in the\n" +
			"operating system's keychain, under the name of the variable that file names, so the key lies\n" +
			"in no shell profile. The distiller reads the keychain first and the environment second, as on\n" +
			"CI, where the pipeline's secret store sets the variable.",
		Args: cobra.NoArgs,
	}
	set := &cobra.Command{
		Use:   "set [NAME]",
		Short: "Keep a provider's key, read from standard input, in your keychain",
		Long: "set keeps the key standard input carries in the keychain under NAME, or, without NAME, under\n" +
			"the variable the active organisation's distiller names. The key comes through standard input\n" +
			"so it shows nowhere and stays out of the shell's history; typed at a terminal it is refused.",
		Example: "  pbpaste | rein key set\n  rein key set DEEPSEEK_API_KEY < key.txt",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			orchestrator, err := environment.orchestrate()
			if err != nil {
				return err
			}
			name, err := keyName(orchestrator, organization, arguments)
			if err != nil {
				return err
			}
			if environment.terminal {
				return refusal.New(fmt.Sprintf("typed here the key would show; pass it through standard input: pbpaste | rein key set %s", name))
			}
			value, err := io.ReadAll(io.LimitReader(environment.in, keyLimit))
			if err != nil {
				return err
			}
			err = orchestrator.Credentials.StoreKey(name, string(value))
			if err != nil {
				return err
			}
			fmt.Fprintf(environment.out, "kept %s in your keychain; the distiller reads it from there\n", name)
			return nil
		},
	}
	remove := &cobra.Command{
		Use:     "remove [NAME]",
		Short:   "Remove a provider's key from your keychain",
		Example: "  rein key remove\n  rein key remove DEEPSEEK_API_KEY",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			orchestrator, err := environment.orchestrate()
			if err != nil {
				return err
			}
			name, err := keyName(orchestrator, organization, arguments)
			if err != nil {
				return err
			}
			err = orchestrator.Credentials.DeleteKey(name)
			if err != nil {
				return err
			}
			fmt.Fprintf(environment.out, "removed %s from your keychain\n", name)
			return nil
		},
	}
	for _, subcommand := range []*cobra.Command{set, remove} {
		subcommand.Flags().StringVar(&organization, "org", "", "the organisation whose distiller names the variable (default: the active one)")
	}
	command.AddCommand(set, remove)
	return command
}

// keyName is the variable named on the command line, else the one the
// organisation's distiller names.
func keyName(orchestrator orchestration.Orchestrator, organization string, arguments []string) (string, error) {
	if len(arguments) == 1 {
		return arguments[0], nil
	}
	return orchestrator.DistillerKeyName(organization)
}
