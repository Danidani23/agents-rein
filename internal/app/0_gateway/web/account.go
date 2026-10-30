package web

import (
	"html/template"
	"net/http"
	"slices"

	"github.com/Danidani23/agents-rein/internal/app/0_gateway/wording"
	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// loginAction starts a login, as rein login does (D58): the run page shows
// the code GitHub asks for with a button to its page, and Cancel ends the
// wait for the approval.
func loginAction(orchestrator orchestration.Orchestrator, version string, actions *runner, page *template.Template) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		report, err := orchestrator.Status()
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		begin(writer, request, actions, "", 0, "Log in", func(current *run) (outcome, error) {
			acting := orchestrator
			acting.Progress = current.record
			result, err := acting.Login(orchestration.LoginRequest{Context: current.ctx, Holder: ports.LockHolder{Command: "rein login", Caller: "the page"}})
			if err != nil {
				return outcome{}, err
			}
			return outcome{Headline: wording.LoggedIn(result.Login), Next: "the organisations connected with this account act through it; one more account logs in the same way"}, nil
		}, func() previewView {
			return previewView{frame: frameOf(request, report, version, "Log in", "machine", actions)}
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
		view := previewView{frame: frameOf(request, report, version, "Log out "+login, "machine", actions), Action: "/actions/logout", Confirm: "Log out", Back: "/", Busy: actions.runningName()}
		if !slices.Contains(report.Logins, login) {
			view.Problem = login + " is not logged in on this machine"
			render(writer, page, view, http.StatusNotFound)
			return
		}
		view.Hidden = []formField{{Name: "login", Value: login}}
		view.Summary = []string{wording.LogOut(login)}
		render(writer, page, view, http.StatusOK)
	})
}

// logoutAction logs an account out, as rein logout does, under the
// machine lock as any change.
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
			problem, status = login+" is not logged in on this machine", http.StatusNotFound
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
			return previewView{frame: frameOf(request, report, version, "Log out "+login, "machine", actions)}
		}, page)
	})
}
