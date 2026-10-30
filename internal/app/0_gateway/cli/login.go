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
		Use:   "login [account]",
		Short: "Log an account of this machine in again through the Agents Rein app; the token goes into your keychain",
		Long: "login runs GitHub's device flow for the Agents Rein app: rein shows a short code and a web\n" +
			"address, you enter the code there and approve the app with the permissions it lists, and\n" +
			"GitHub hands rein a token that acts as you. The token lives in the operating system's\n" +
			"keychain, never in a file of rein's, and is renewed by itself. login renews the token of\n" +
			"an account on this machine, the only one when none is named; on a machine without any\n" +
			"account it adds the one you approve as. rein account add adds another account.",
		Example: "  rein login\n  rein login Danidani23",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			// the login takes the machine lock only to keep the token, not while you approve (D59)
			orchestrator, err := environment.orchestrate()
			if err != nil {
				return err
			}
			request := orchestration.LoginRequest{Context: environment.ctx, Holder: environment.lockHolder()}
			if len(arguments) == 1 {
				request.Account = arguments[0]
			}
			orchestrator.Progress = environment.printProgress
			result, err := orchestrator.Login(request)
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
		Use:   "logout [account]",
		Short: "Delete an account's token from your keychain; the account stays, logged out",
		Long: "logout deletes the token of an account, the only one when none is named, from your\n" +
			"keychain. The account stays on this machine, logged out, and the organisations that act\n" +
			"with it wait until rein login logs it in again; rein account remove forgets it.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			orchestrator, release, err := environment.orchestrateLocked()
			if err != nil {
				return err
			}
			defer release()
			login := ""
			if len(arguments) == 1 {
				login = arguments[0]
			}
			if login == "" {
				logins, err := orchestrator.Credentials.Logins()
				if err != nil {
					return err
				}
				switch len(logins) {
				case 0:
					return refusal.New("no account on this machine to log out")
				case 1:
					login = logins[0]
				default:
					return refusal.New(fmt.Sprintf("several accounts are on this machine, %s; name the one to log out: rein logout <account>", strings.Join(logins, ", ")))
				}
			}
			err = orchestrator.Credentials.Logout(login)
			if err != nil {
				return err
			}
			fmt.Fprintf(environment.out, "%s\n", wording.LoggedOut(login))
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
