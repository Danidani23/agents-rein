package cli

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/app/0_gateway/wording"
	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
)

func newLoginCommand(environment *commandEnvironment) *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Log in to GitHub through the Agents Rein app; the token goes into your keychain",
		Long: "login runs GitHub's device flow for the Agents Rein app: rein shows a short code and a web\n" +
			"address, you enter the code there and approve the app with the permissions it lists, and\n" +
			"GitHub hands rein a token that acts as you. The token lives in the operating system's\n" +
			"keychain, never in a file of rein's, and is renewed by itself. Log in once per GitHub\n" +
			"account you use; each organisation remembers the account that connected it.",
		Example: "  rein login",
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			// the login takes the machine lock only to keep the token, not while you approve (D59)
			orchestrator, err := environment.orchestrate()
			if err != nil {
				return err
			}
			orchestrator.Progress = environment.printProgress
			result, err := orchestrator.Login(orchestration.LoginRequest{Context: environment.ctx, Holder: environment.lockHolder()})
			if err != nil {
				return err
			}
			fmt.Fprintf(environment.out, "%s\n", wording.LoggedIn(result.Login))
			return nil
		},
	}
}

func newLogoutCommand(environment *commandEnvironment) *cobra.Command {
	return &cobra.Command{
		Use:   "logout <login>",
		Short: "Forget a GitHub account and delete its token from your keychain",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			orchestrator, release, err := environment.orchestrateLocked()
			if err != nil {
				return err
			}
			defer release()
			err = orchestrator.Credentials.Logout(arguments[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(environment.out, "%s\n", wording.LoggedOut(arguments[0]))
			return nil
		},
	}
}

// newGitCredentialCommand is the credential helper rein's own git calls
// use: git asks it for the token of the login named in REIN_LOGIN, which
// the git adapter sets for the operation (D25). It is hidden, since only
// git calls it.
func newGitCredentialCommand(environment *commandEnvironment) *cobra.Command {
	return &cobra.Command{
		Use:    "git-credential <get|store|erase>",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			if arguments[0] != "get" {
				return nil
			}
			login := environment.runner.Getenv("REIN_LOGIN")
			if login == "" {
				return refusal.New("git-credential is for rein's own git calls; REIN_LOGIN is not set")
			}
			scanner := bufio.NewScanner(environment.in)
			for scanner.Scan() {
				if strings.TrimSpace(scanner.Text()) == "" {
					break
				}
			}
			orchestrator, err := environment.orchestrate()
			if err != nil {
				return err
			}
			accessToken, err := orchestrator.Credentials.AccessToken(login)
			if err != nil {
				return err
			}
			fmt.Fprintf(environment.out, "username=x-access-token\npassword=%s\n", accessToken)
			return nil
		},
	}
}
