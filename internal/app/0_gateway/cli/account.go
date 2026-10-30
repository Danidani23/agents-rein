package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/app/0_gateway/wording"
	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// hostChoices are the hosts an account or an organisation can be on: the
// supported ones, and the planned ones listed and refused.
var hostChoices = []ports.Choice{
	{Name: "github", Description: "GitHub, github.com"},
	{Name: "gitlab", Description: "GitLab", Unavailable: "planned, not yet supported"},
}

func newAccountCommand(environment *commandEnvironment) *cobra.Command {
	account := &cobra.Command{
		Use:   "account",
		Short: "The host accounts rein acts as on this machine: add, list and remove them",
		Long: "An account is your account on a host, GitHub today, that rein acts as on this machine. Its\n" +
			"token lives in your keychain. Every organisation acts with the account that connected or\n" +
			"created it; rein logout deletes an account's token and keeps the account, and rein login\n" +
			"logs it in again.",
	}
	account.AddCommand(newAccountAddCommand(environment), newAccountListCommand(environment), newAccountRemoveCommand(environment))
	return account
}

func newAccountAddCommand(environment *commandEnvironment) *cobra.Command {
	var host string
	command := &cobra.Command{
		Use:   "add",
		Short: "Add an account by logging in to its host; the token goes into your keychain",
		Long: "add runs the host's device flow for the Agents Rein app: rein shows a short code and a web\n" +
			"address, you enter the code there and approve the app with the permissions it lists, and\n" +
			"the account you approve as is added to this machine, its token in your keychain. On a\n" +
			"terminal rein asks for the host; GitLab is listed, planned and refused.",
		Example: "  rein account add\n  rein account add --host github",
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			// the login takes the machine lock only to keep the token, not while you approve (D59)
			orchestrator, err := environment.orchestrate()
			if err != nil {
				return err
			}
			prompter := environment.prompter()
			if host == "" && prompter.CanAsk() {
				host, err = prompter.Choose(ports.Select{
					Title:       "Host",
					Description: "Where the account is: " + hostPhrase + ".",
					Explanation: "rein acts as this account on the host: it clones the organisations' repositories, pushes to them and opens pull requests there as you.",
					Choices:     hostChoices,
					Default:     "github",
				})
				if err != nil {
					return refusal.New(err.Error())
				}
			}
			if host == "" {
				host = "github"
			}
			orchestrator.Progress = environment.printProgress
			result, err := orchestrator.AddAccount(orchestration.AddAccountRequest{Host: host, Context: environment.ctx, Holder: environment.lockHolder()})
			if err != nil {
				return err
			}
			fmt.Fprintf(environment.out, "%s\n", wording.AccountAdded(result))
			return nil
		},
	}
	command.Flags().StringVar(&host, "host", "", "the account's host: github; gitlab is planned (default: asked on a terminal, else github)")
	return command
}

func newAccountListCommand(environment *commandEnvironment) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the accounts on this machine, whether each is logged in, and what acts with it",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			orchestrator, err := environment.orchestrate()
			if err != nil {
				return err
			}
			report, err := orchestrator.Status()
			if err != nil {
				return err
			}
			if len(report.Accounts) == 0 {
				fmt.Fprintf(environment.out, "no account on this machine; rein account add adds one\n")
				return nil
			}
			width := 0
			for _, account := range report.Accounts {
				width = max(width, len(account.Login))
			}
			for _, account := range report.Accounts {
				standing := "logged in"
				if !account.LoggedIn {
					standing = "logged out, " + account.Reason
				}
				acting := ""
				if len(account.Organizations) > 0 {
					acting = " · acts for " + strings.Join(account.Organizations, ", ")
				}
				fmt.Fprintf(environment.out, "%-*s  %s · %s%s\n", width, account.Login, wording.HostName(account.Host), standing, acting)
			}
			return nil
		},
	}
}

func newAccountRemoveCommand(environment *commandEnvironment) *cobra.Command {
	var yes bool
	command := &cobra.Command{
		Use:   "remove <account>",
		Short: "Delete an account's token and forget the account; refused while an organisation acts with it",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			orchestrator, release, err := environment.orchestrateLocked()
			if err != nil {
				return err
			}
			defer release()
			plan, err := orchestrator.PlanRemoveAccount(arguments[0])
			if err != nil {
				return err
			}
			summary, refused := wording.RemoveAccount(plan)
			if refused != "" {
				return refusal.New(refused)
			}
			fmt.Fprintf(environment.out, "%s\n", summary)
			err = environment.confirmChange([]string{summary}, "Remove the account?", "The account's token is deleted from your keychain; rein account add adds it again.", yes)
			if err != nil {
				return err
			}
			orchestrator.Progress = environment.printProgress
			result, err := orchestrator.RemoveAccount(arguments[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(environment.out, "%s\n", wording.AccountRemoved(result.Login))
			return nil
		},
	}
	command.Flags().BoolVar(&yes, "yes", false, "do not ask for confirmation on a terminal")
	return command
}
