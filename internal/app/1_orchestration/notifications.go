package orchestration

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// The kinds of notification besides the check's findings (D63).
const (
	NotificationConfirmation = "confirmation"
	NotificationRelease      = "release"
)

// Notification is one thing that needs you (D63): its kind, the account it
// belongs to, "" for the machine, what it is about, what it says, the page
// of rein's window that shows it, and since when it holds, zero where that
// is not known.
type Notification struct {
	Kind    string
	Account string
	Subject string
	Detail  string
	Page    string
	Since   time.Time
}

// NotificationsOf is what needs you on the machine report describes, with
// the confirmations waiting in the app's window: each account logged out,
// each confirmation, a release of rein newer than this one, and each open
// finding of the check but an automatic sync done, which needs nothing.
// The CLI and the window both list it through this one rule.
func NotificationsOf(report StatusReport, waiting []ports.Confirmation) []Notification {
	var notifications []Notification
	loggedOut := map[string]bool{}
	for _, account := range report.Accounts {
		if !account.LoggedIn {
			loggedOut[account.Login] = true
			notifications = append(notifications, Notification{Kind: machine.FindingLoggedOut, Account: account.Login, Subject: account.Login, Detail: account.Reason + "; log in again", Page: "/accounts"})
		}
	}
	for _, finding := range report.Findings {
		if finding.Kind == machine.FindingLoggedOut && !loggedOut[finding.Subject] && slices.ContainsFunc(report.Accounts, func(account AccountStatus) bool { return account.Login == finding.Subject }) {
			loggedOut[finding.Subject] = true
			notifications = append(notifications, Notification{Kind: finding.Kind, Account: finding.Subject, Subject: finding.Subject, Detail: finding.Detail, Page: finding.Page, Since: finding.Since})
		}
	}
	for _, asked := range waiting {
		notifications = append(notifications, Notification{Kind: NotificationConfirmation, Account: accountOfFolder(report, asked.Folder), Subject: asked.Command, Detail: "from " + asked.Caller + " in " + asked.Folder + ", waiting for your confirmation", Page: "/confirmations", Since: asked.Asked})
	}
	if report.Release != "" {
		notifications = append(notifications, Notification{Kind: NotificationRelease, Subject: "rein " + report.Release, Detail: fmt.Sprintf("rein %s is out; this is %s", report.Release, report.Version), Page: "/"})
	}
	for _, finding := range report.Findings {
		if finding.Kind == machine.FindingSynced || finding.Kind == machine.FindingLoggedOut {
			continue
		}
		notifications = append(notifications, Notification{Kind: finding.Kind, Account: accountOfFolder(report, finding.Subject), Subject: finding.Subject, Detail: finding.Detail, Page: finding.Page, Since: finding.Since})
	}
	return notifications
}

// ForAccount narrows notifications to one account's and the machine's own;
// "" keeps every one.
func ForAccount(notifications []Notification, account string) []Notification {
	if account == "" {
		return notifications
	}
	var narrowed []Notification
	for _, notification := range notifications {
		if notification.Account == account || notification.Account == "" {
			narrowed = append(narrowed, notification)
		}
	}
	return narrowed
}

// FilterByAccount narrows a report to one account (D62): its row of the
// accounts, the organisations that act with it, their clones and the
// proposals waiting in them; the machine's own parts stay. "" keeps all.
func FilterByAccount(report StatusReport, account string) StatusReport {
	if account == "" {
		return report
	}
	narrowed := report
	narrowed.Accounts, narrowed.Organizations, narrowed.ProjectClones, narrowed.Clones, narrowed.Pending = nil, nil, nil, nil, nil
	for _, status := range report.Accounts {
		if status.Login == account {
			narrowed.Accounts = append(narrowed.Accounts, status)
		}
	}
	kept := map[string]bool{}
	for _, organization := range report.Organizations {
		if ActingAccount(organization, report.Accounts) == account {
			kept[organization.ID] = true
			narrowed.Organizations = append(narrowed.Organizations, organization)
		}
	}
	for _, clone := range report.ProjectClones {
		if kept[clone.Organization] {
			narrowed.ProjectClones = append(narrowed.ProjectClones, clone)
		}
	}
	for _, clone := range report.Clones {
		if kept[clone.Organization] {
			narrowed.Clones = append(narrowed.Clones, clone)
		}
	}
	for _, pending := range report.Pending {
		owner := cloneHolding(report, pending.Path)
		if owner == nil || kept[owner.Organization] {
			narrowed.Pending = append(narrowed.Pending, pending)
		}
	}
	return narrowed
}

// ActingAccount is the account an organisation acts with: the one that
// connected it, or the only account for one connected before organisations
// remembered theirs.
func ActingAccount(organization machine.OrganizationRef, accounts []AccountStatus) string {
	if organization.Login != "" || len(accounts) != 1 {
		return organization.Login
	}
	return accounts[0].Login
}

// accountOfFolder is the account of the organisation whose project clone
// holds folder, "" for a folder in no clone.
func accountOfFolder(report StatusReport, folder string) string {
	clone := cloneHolding(report, folder)
	if clone == nil {
		return ""
	}
	for _, organization := range report.Organizations {
		if organization.ID == clone.Organization {
			return ActingAccount(organization, report.Accounts)
		}
	}
	return ""
}

// cloneHolding is the project clone folder is or lies in, nil for none.
func cloneHolding(report StatusReport, folder string) *machine.ProjectClone {
	for index, clone := range report.ProjectClones {
		if folder == clone.ClonePath || strings.HasPrefix(folder, clone.ClonePath+string(filepath.Separator)) {
			return &report.ProjectClones[index]
		}
	}
	return nil
}
