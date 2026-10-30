package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
)

func newNotificationsCommand(environment *commandEnvironment) *cobra.Command {
	var account string
	command := &cobra.Command{
		Use:   "notifications",
		Short: "List what needs you: accounts logged out, confirmations waiting, a new rein, what the check found",
		Long: "notifications lists what needs you on this machine (D63), as the app's Notifications page\n" +
			"does and its menu bar icon counts: each account logged out, each confirmation waiting in\n" +
			"the app's window, a release of rein newer than this one, and each open finding of the\n" +
			"check. Each names its account, or this machine; --account lists one account's and this\n" +
			"machine's own. It changes nothing; rein check looks again.",
		Example: "  rein notifications\n  rein notifications --account Danidani23",
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			// a read never waits for the machine lock (R1, D56)
			orchestrator, err := environment.orchestrate()
			if err != nil {
				return err
			}
			request := orchestration.NotificationsRequest{Account: account}
			if environment.confirmer != nil {
				request.Waiting = environment.confirmer.Waiting()
			}
			notifications, err := orchestrator.Notifications(request)
			if err != nil {
				return err
			}
			if len(notifications) == 0 {
				fmt.Fprintf(environment.out, "nothing needs you\n")
				return nil
			}
			fmt.Fprintf(environment.out, "%s\n", counted(len(notifications), "thing needs you", "things need you"))
			kindWidth, accountWidth := 0, 0
			for _, notification := range notifications {
				kindWidth = max(kindWidth, len(notification.Kind))
				accountWidth = max(accountWidth, len(accountLabel(notification.Account)))
			}
			for _, notification := range notifications {
				fmt.Fprintf(environment.out, "  %-*s  %-*s  %s: %s\n", kindWidth, notification.Kind, accountWidth, accountLabel(notification.Account), shortPath(notification.Subject), notification.Detail)
			}
			return nil
		},
	}
	command.Flags().StringVar(&account, "account", "", "list one account's and this machine's own (default: every account)")
	return command
}

// accountLabel names a notification's account, or the machine for one of
// its own.
func accountLabel(account string) string {
	if account == "" {
		return "this machine"
	}
	return account
}
