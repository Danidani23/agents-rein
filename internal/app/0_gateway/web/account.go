package web

import (
	"html/template"
	"net/http"
	"slices"
	"strings"

	"github.com/Danidani23/agents-rein/internal/app/0_gateway/wording"
	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// accountsPage shows the accounts of this machine, whether each is logged
// in, the organisations that act with each, and what can be done with one
// (D61), as rein account list shows them.
func accountsPage(orchestrator orchestration.Orchestrator, version string, actions *runner, page *template.Template) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		report, err := orchestrator.Status()
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		view := accountsView{frame: frameOf(request, report, version, "Accounts", "accounts", actions)}
		for _, account := range report.Accounts {
			view.Accounts = append(view.Accounts, accountRow{Login: account.Login, Host: wording.HostName(account.Host), LoggedIn: account.LoggedIn, Reason: account.Reason, Organizations: strings.Join(account.Organizations, ", ")})
		}
		render(writer, page, view, http.StatusOK)
	})
}

// accountsView is what the Accounts page shows in its frame.
type accountsView struct {
	frame
	Accounts []accountRow
}

// accountRow is one account of this machine.
type accountRow struct {
	Login         string
	Host          string
	LoggedIn      bool
	Reason        string
	Organizations string
}

// accountAddPreview asks for the host, as rein account add does on a
// terminal: GitHub, and GitLab listed, planned and refused.
func accountAddPreview(orchestrator orchestration.Orchestrator, version string, actions *runner, page *template.Template) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		report, err := orchestrator.Status()
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		view := previewView{frame: frameOf(request, report, version, "Add account", "accounts", actions), Action: "/actions/account-add", Confirm: "Log in to add", Back: "/accounts", Busy: actions.runningName()}
		view.Summary = []string{"rein asks the host for a code; you enter it on the host's page and approve the Agents Rein app, and the account you approve as is added to this machine, its token in your keychain."}
		view.Choices = []formChoice{
			{Name: "host", Value: "github", Label: "GitHub", Note: "github.com", Checked: true},
			{Name: "host", Value: "gitlab", Label: "GitLab", Note: "planned, not yet supported", Disabled: true},
		}
		render(writer, page, view, http.StatusOK)
	})
}

// accountAddAction adds an account, as rein account add does (D58): the
// run page shows the code the host asks for with a button to its page, and
// Cancel ends the wait for the approval.
func accountAddAction(orchestrator orchestration.Orchestrator, version string, actions *runner, page *template.Template) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		report, err := orchestrator.Status()
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		host := request.FormValue("host")
		begin(writer, request, actions, "", 0, "Add account", func(current *run) (outcome, error) {
			acting := orchestrator
			acting.Progress = current.record
			result, err := acting.AddAccount(orchestration.AddAccountRequest{Host: host, Context: current.ctx, Holder: ports.LockHolder{Command: "rein account add", Caller: "the page"}})
			if err != nil {
				return outcome{}, err
			}
			return outcome{Headline: wording.AccountAdded(result), Next: "the organisations you create or connect with this account act through it"}, nil
		}, func() previewView {
			return previewView{frame: frameOf(request, report, version, "Add account", "accounts", actions)}
		}, page)
	})
}

// loginAction logs an account in again, as rein login does (D58), or adds
// the first one on a machine without any.
func loginAction(orchestrator orchestration.Orchestrator, version string, actions *runner, page *template.Template) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		report, err := orchestrator.Status()
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		account := request.FormValue("account")
		problem, status := "", 0
		if account != "" && !slices.Contains(report.Logins, account) {
			problem, status = account+" is not an account on this machine", http.StatusNotFound
		}
		name := "Log in"
		if account != "" {
			name = "Log in " + account
		}
		begin(writer, request, actions, problem, status, name, func(current *run) (outcome, error) {
			acting := orchestrator
			acting.Progress = current.record
			result, err := acting.Login(orchestration.LoginRequest{Account: account, Context: current.ctx, Holder: ports.LockHolder{Command: "rein login " + account, Caller: "the page"}})
			if err != nil {
				return outcome{}, err
			}
			return outcome{Headline: wording.LoggedIn(result.Login), Next: "the organisations that act with this account act through it again"}, nil
		}, func() previewView {
			return previewView{frame: frameOf(request, report, version, name, "accounts", actions)}
		}, page)
	})
}

// logoutPreview says what logging an account out does, as the CLI's
// logout would, and holds the form.
func logoutPreview(orchestrator orchestration.Orchestrator, version string, actions *runner, page *template.Template) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		report, err := orchestrator.Status()
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		login := request.FormValue("login")
		view := previewView{frame: frameOf(request, report, version, "Log out "+login, "accounts", actions), Action: "/actions/logout", Confirm: "Log out", Back: "/accounts", Busy: actions.runningName()}
		if !slices.Contains(report.Logins, login) {
			view.Problem = login + " is not an account on this machine"
			render(writer, page, view, http.StatusNotFound)
			return
		}
		view.Hidden = []formField{{Name: "login", Value: login}}
		view.Summary = []string{wording.LogOut(login)}
		render(writer, page, view, http.StatusOK)
	})
}

// logoutAction logs an account out, as rein logout does, under the
// machine lock as any change; the account stays, logged out.
func logoutAction(orchestrator orchestration.Orchestrator, version string, actions *runner, page *template.Template) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		report, err := orchestrator.Status()
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		login := request.FormValue("login")
		problem, status := "", 0
		if !slices.Contains(report.Logins, login) {
			problem, status = login+" is not an account on this machine", http.StatusNotFound
		}
		begin(writer, request, actions, problem, status, "Log out "+login, func(current *run) (done outcome, err error) {
			release, err := lockFor(current, orchestrator, "rein logout "+login, "")
			if err != nil {
				return joinedOutcome(err)
			}
			defer func() { release(err) }()
			err = orchestrator.Credentials.Logout(login)
			if err != nil {
				return outcome{}, err
			}
			return outcome{Headline: wording.LoggedOut(login)}, nil
		}, func() previewView {
			return previewView{frame: frameOf(request, report, version, "Log out "+login, "accounts", actions)}
		}, page)
	})
}

// accountRemovePreview says what removing an account does, or why it
// cannot be removed yet, from the plan rein account remove shows (D60).
func accountRemovePreview(orchestrator orchestration.Orchestrator, version string, actions *runner, page *template.Template) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		report, err := orchestrator.Status()
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		login := request.FormValue("login")
		view := previewView{frame: frameOf(request, report, version, "Remove "+login, "accounts", actions), Action: "/actions/account-remove", Confirm: "Remove the account", Back: "/accounts", Busy: actions.runningName()}
		if !slices.Contains(report.Logins, login) {
			view.Problem = login + " is not an account on this machine"
			render(writer, page, view, http.StatusNotFound)
			return
		}
		plan, err := orchestrator.PlanRemoveAccount(login)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		summary, refused := wording.RemoveAccount(plan)
		if refused != "" {
			view.Problem = strings.TrimSuffix(refused, ".")
			render(writer, page, view, http.StatusConflict)
			return
		}
		view.Hidden = []formField{{Name: "login", Value: login}}
		view.Summary = []string{summary}
		render(writer, page, view, http.StatusOK)
	})
}

// accountRemoveAction removes an account, as rein account remove does,
// under the machine lock as any change.
func accountRemoveAction(orchestrator orchestration.Orchestrator, version string, actions *runner, page *template.Template) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		report, err := orchestrator.Status()
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		login := request.FormValue("login")
		problem, status := "", 0
		if !slices.Contains(report.Logins, login) {
			problem, status = login+" is not an account on this machine", http.StatusNotFound
		}
		begin(writer, request, actions, problem, status, "Remove "+login, func(current *run) (done outcome, err error) {
			release, err := lockFor(current, orchestrator, "rein account remove "+login, "")
			if err != nil {
				return joinedOutcome(err)
			}
			defer func() { release(err) }()
			acting := orchestrator
			acting.Progress = current.record
			result, err := acting.RemoveAccount(login)
			if err != nil {
				return outcome{}, err
			}
			return outcome{Headline: wording.AccountRemoved(result.Login)}, nil
		}, func() previewView {
			return previewView{frame: frameOf(request, report, version, "Remove "+login, "accounts", actions)}
		}, page)
	})
}
