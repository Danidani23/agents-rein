package web

import (
	"html/template"
	"net/http"

	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
)

// notificationsPage lists what needs you (D63), as rein notifications
// does, narrowed to the account the top bar chose.
func notificationsPage(orchestrator orchestration.Orchestrator, version string, actions *runner, page *template.Template) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		report, err := orchestrator.Status()
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		view := notificationsView{frame: frameOf(request, report, version, "Notifications", "notifications", actions)}
		for _, notification := range orchestration.ForAccount(orchestration.NotificationsOf(report, actions.confirmations.Waiting()), view.ChosenAccount) {
			row := notificationRow{Kind: notification.Kind, Account: notification.Account, Subject: shortPath(notification.Subject), Detail: notification.Detail, Link: notification.Page}
			if !notification.Since.IsZero() {
				row.Since = notification.Since.Local().Format(timeLayout)
			}
			// the window has no Update; the browser's This machine has
			if notification.Kind == orchestration.NotificationRelease && view.InWindow {
				row.Link = ""
			}
			view.Notifications = append(view.Notifications, row)
		}
		render(writer, page, view, http.StatusOK)
	})
}

// notificationsView is what the Notifications page shows in its frame.
type notificationsView struct {
	frame
	Notifications []notificationRow
}

// notificationRow is one thing that needs you, with the page that shows it.
type notificationRow struct {
	Kind    string
	Account string
	Subject string
	Detail  string
	Since   string
	Link    string
}
