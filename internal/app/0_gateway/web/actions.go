package web

import (
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"strings"

	"github.com/Danidani23/agents-rein/internal/app/0_gateway/wording"
	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// Every action follows one pattern (D46): its preview, reached by a link,
// says what will change, as the CLI does before it asks, and holds the
// form; confirming the form starts the action and leads to its run.

// previewView is an action's preview: what it will do, what the developer
// should know first, the choices the CLI offers as flags, and the form
// that confirms it. Busy names the action running now, which holds the
// form back.
type previewView struct {
	frame
	Action       string
	Summary      []string
	ItemsHeading string
	Items        []string
	Hidden       []formField
	Options      []formOption
	Choices      []formChoice
	Confirm      string
	Back         string
	Busy         string
}

// formField is a value the form sends back as it was shown.
type formField struct {
	Name  string
	Value string
}

// formOption is a box for one of the CLI's flags.
type formOption struct {
	Name  string
	Label string
}

// formChoice is one answer to a question the CLI asks on a terminal, of
// which the form sends the one chosen; a disabled one is shown and refused.
type formChoice struct {
	Name     string
	Value    string
	Label    string
	Note     string
	Checked  bool
	Disabled bool
}

// lockFor waits for the action's turn at the machine lock, as the CLI's
// writing commands wait (D56), showing what it waits for as a step; Cancel
// leaves the line. command and folder are the CLI's command it does, so an
// identical command in line is joined (R3).
func lockFor(current *run, orchestrator orchestration.Orchestrator, command string, folder string) (release func(outcome error), err error) {
	if orchestrator.Lock == nil {
		return func(error) {}, nil
	}
	release, err = orchestrator.Lock.Acquire(current.ctx, ports.LockHolder{Command: command, Caller: "the page", Folder: folder}, current.waitFor)
	current.waited()
	return release, err
}

// joinedOutcome is the end of an action that ran not at all because the
// same one was in line: done when that one worked, its failure otherwise.
func joinedOutcome(err error) (outcome, error) {
	var joined ports.Joined
	if errors.As(err, &joined) && joined.Outcome == nil {
		return outcome{Headline: "done: " + joined.Error()}, nil
	}
	return outcome{}, err
}

// actionTarget is what an action's request names, checked against what
// this machine has: a project clone rein initialised, or every clone.
type actionTarget struct {
	report orchestration.StatusReport
	clone  orchestration.CloneStatus
	all    bool
}

// targetOf reads the request's folder, or all, and refuses what this
// machine does not have or a developer not logged in; status is what the
// refusal answers with.
func targetOf(orchestrator orchestration.Orchestrator, request *http.Request, allowAll bool) (target actionTarget, problem string, status int, err error) {
	target.report, err = orchestrator.Status()
	if err != nil {
		return actionTarget{}, "", 0, err
	}
	if len(target.report.Logins) == 0 {
		return target, "not logged in; Log in on This machine, which opens GitHub's device flow", http.StatusForbidden, nil
	}
	if allowAll && request.FormValue("all") == "on" {
		target.all = true
		return target, "", 0, nil
	}
	folder := request.FormValue("folder")
	for _, clone := range target.report.Clones {
		if clone.ClonePath == folder {
			target.clone = clone
			return target, "", 0, nil
		}
	}
	return target, folder + " is not a project clone rein initialised on this machine", http.StatusNotFound, nil
}

// projectOf is the project slug of a clone's address.
func projectOf(clone orchestration.CloneStatus) string {
	return clone.Address[strings.LastIndex(clone.Address, ".")+1:]
}

// syncPreview says what sync will write and names the files a hand edit
// changed, which stop it unless the developer ticks force.
func syncPreview(orchestrator orchestration.Orchestrator, version string, actions *runner, page *template.Template) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		target, problem, status, err := targetOf(orchestrator, request, true)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		view := previewView{frame: frameOf(request, target.report, version, "Sync", "projects", actions), Action: "/actions/sync", Confirm: "Sync", Back: "/projects", Busy: actions.runningName()}
		if problem != "" {
			view.Problem = problem
			render(writer, page, view, status)
			return
		}
		view.Options = []formOption{{Name: "force", Label: "Overwrite the files edited by hand, after keeping them in a snapshot (--force)"}}
		if target.all {
			view.Title = "Sync every clone"
			view.Hidden = []formField{{Name: "all", Value: "on"}}
			view.Summary = []string{"This will pull each organisation's repository and sync every project clone on this machine, as rein sync --all does. A clone that cannot be synced is reported; the others are written."}
			view.ItemsHeading = "The clones"
			for _, clone := range target.report.Clones {
				view.Items = append(view.Items, projectOf(clone)+" · "+shortPath(clone.ClonePath))
			}
			render(writer, page, view, http.StatusOK)
			return
		}
		view.Title = "Sync " + projectOf(target.clone)
		view.Back = "/projects?project=" + projectOf(target.clone)
		view.Hidden = []formField{{Name: "folder", Value: target.clone.ClonePath}}
		view.Summary = []string{fmt.Sprintf("This will pull the organisation's repository, resolve what %s's settings are, and write them into %s: CLAUDE.md with one imported file per level, rules, skills and hook scripts under .claude/, permissions and hooks in .claude/settings.local.json, and tool servers in .mcp.json. Every file is recorded with its hash.", projectOf(target.clone), shortPath(target.clone.ClonePath))}
		files, err := orchestrator.SyncedFiles(target.clone.ClonePath)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		for _, file := range files {
			if file.State == orchestration.SyncedEditedByHand {
				view.Items = append(view.Items, file.Path)
			}
		}
		if len(view.Items) > 0 {
			view.ItemsHeading = "Edited by hand since the last sync: sync stops on these unless you tick the box below"
		}
		render(writer, page, view, http.StatusOK)
	})
}

// syncAction starts sync of the clone named, or of every clone.
func syncAction(orchestrator orchestration.Orchestrator, version string, actions *runner, page *template.Template) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		target, problem, status, err := targetOf(orchestrator, request, true)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		name := "Sync every clone"
		if !target.all {
			name = "Sync " + projectOf(target.clone)
		}
		syncRequest := orchestration.SyncRequest{Directory: target.clone.ClonePath, All: target.all, Force: request.FormValue("force") == "on"}
		begin(writer, request, actions, problem, status, name, func(current *run) (done outcome, err error) {
			acting := orchestrator
			acting.Progress = current.record
			release, err := lockFor(current, acting, syncCommand(syncRequest), syncRequest.Directory)
			if err != nil {
				return joinedOutcome(err)
			}
			defer func() { release(err) }()
			syncRequest.ReinExecutable, err = os.Executable()
			if err != nil {
				return outcome{}, err
			}
			result, err := acting.SyncClone(syncRequest)
			return syncOutcome(result), err
		}, func() previewView {
			return previewView{frame: frameOf(request, target.report, version, name, "projects", actions)}
		}, page)
	})
}

// resetPreview says what reset project will put back, as the CLI does
// before it asks.
func resetPreview(orchestrator orchestration.Orchestrator, version string, actions *runner, page *template.Template) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		target, problem, status, err := targetOf(orchestrator, request, false)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		view := previewView{frame: frameOf(request, target.report, version, "Reset project", "projects", actions), Action: "/actions/reset-project", Confirm: "Reset project", Back: "/projects", Busy: actions.runningName()}
		if problem != "" {
			view.Problem = problem
			render(writer, page, view, status)
			return
		}
		view.Title = "Reset " + projectOf(target.clone)
		view.Back = "/projects?project=" + projectOf(target.clone)
		view.Hidden = []formField{{Name: "folder", Value: target.clone.ClonePath}}
		summary, explanation := wording.ResetProject(shortPath(target.clone.ClonePath))
		view.Summary = []string{summary, explanation}
		view.Options = []formOption{
			{Name: "no-commit", Label: "Leave the put-back staged, not committed (--no-commit)"},
			{Name: "force", Label: "Overwrite the files in the way of the put-back (--force)"},
		}
		render(writer, page, view, http.StatusOK)
	})
}

// resetAction starts reset project of the clone named.
func resetAction(orchestrator orchestration.Orchestrator, version string, actions *runner, page *template.Template) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		target, problem, status, err := targetOf(orchestrator, request, false)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		name := "Reset " + projectOf(target.clone)
		restoreRequest := orchestration.RestoreRequest{Directory: target.clone.ClonePath, NoCommit: request.FormValue("no-commit") == "on", Force: request.FormValue("force") == "on"}
		begin(writer, request, actions, problem, status, name, func(current *run) (done outcome, err error) {
			acting := orchestrator
			acting.Progress = current.record
			release, err := lockFor(current, acting, "rein reset project", restoreRequest.Directory)
			if err != nil {
				return joinedOutcome(err)
			}
			defer func() { release(err) }()
			result, err := acting.RestoreProject(restoreRequest)
			if err != nil {
				return outcome{}, err
			}
			return restoreOutcome(result), nil
		}, func() previewView {
			return previewView{frame: frameOf(request, target.report, version, name, "projects", actions)}
		}, page)
	})
}

// begin starts a confirmed action and leads to its run, or shows why it
// cannot start: a problem with what the request names, or another action
// running.
func begin(writer http.ResponseWriter, request *http.Request, actions *runner, problem string, status int, name string, action work, refused func() previewView, page *template.Template) {
	if problem == "" {
		_, err := actions.start(name, action)
		if err == nil {
			http.Redirect(writer, request, "/run", http.StatusSeeOther)
			return
		}
		problem, status = err.Error(), http.StatusConflict
	}
	view := refused()
	view.Problem = problem
	render(writer, page, view, status)
}

// autoSyncAction turns a clone's automatic sync on or off, as rein
// auto-sync does (D48), after waiting for its turn at the machine lock as
// any change does (D56), and leads back to the page it came from.
func autoSyncAction(orchestrator orchestration.Orchestrator) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		folder := request.FormValue("folder")
		switched := request.FormValue("on")
		if switched != "on" && switched != "off" {
			http.Error(writer, "automatic sync is on or off", http.StatusBadRequest)
			return
		}
		release := func(error) {}
		if orchestrator.Lock != nil {
			var err error
			release, err = orchestrator.Lock.Acquire(request.Context(), ports.LockHolder{Command: "rein auto-sync " + switched, Caller: "the page", Folder: folder}, nil)
			if err != nil {
				http.Error(writer, err.Error(), http.StatusConflict)
				return
			}
		}
		found, err := orchestrator.Organizations.SetCloneSetting(folder, machine.SettingAutoSync, switched == "on")
		release(err)
		switch {
		case err != nil:
			http.Error(writer, err.Error(), http.StatusInternalServerError)
		case !found:
			http.Error(writer, folder+" is not a project clone rein initialised on this machine", http.StatusNotFound)
		default:
			back := request.Referer()
			if back == "" {
				back = "/projects"
			}
			http.Redirect(writer, request, back, http.StatusSeeOther)
		}
	})
}

// syncCommand is the CLI's command a sync from the page does.
func syncCommand(syncRequest orchestration.SyncRequest) string {
	command := "rein sync"
	if syncRequest.All {
		command += " --all"
	}
	if syncRequest.Force {
		command += " --force"
	}
	return command
}

// syncOutcome is what sync did, in the words the CLI prints (D60).
func syncOutcome(result orchestration.SyncResult) outcome {
	done := outcome{Headline: "done", Next: wording.SyncNext}
	for _, synced := range result.Clones {
		if synced.Skipped != "" {
			done.Lines = append(done.Lines, outcomeLine{Text: "skipped " + shortPath(synced.Folder) + ": " + synced.Skipped, Warning: true})
			continue
		}
		done.Lines = append(done.Lines, outcomeLine{Text: fmt.Sprintf("synced %s for %s: %d written, %d unchanged, %d removed", synced.Address, synced.Agent, len(synced.Written), len(synced.Unchanged), len(synced.Removed))})
		done.Lines = append(done.Lines, linesOf(wording.SyncedNotes(synced))...)
	}
	return done
}

// linesOf are the sentences of what a command did, as the page lists them,
// without the blank lines that part them in a terminal.
func linesOf(lines []wording.Line) []outcomeLine {
	var listed []outcomeLine
	for _, line := range lines {
		if line.Text == "" {
			continue
		}
		listed = append(listed, outcomeLine{Text: line.Text, Warning: line.Warning})
	}
	return listed
}

// distillPreview says what distill from a clone does.
func distillPreview(orchestrator orchestration.Orchestrator, version string, actions *runner, page *template.Template) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		target, problem, status, err := targetOf(orchestrator, request, false)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		view := previewView{frame: frameOf(request, target.report, version, "Distill", "projects", actions), Action: "/actions/distill", Confirm: "Distill", Back: "/projects", Busy: actions.runningName()}
		if problem != "" {
			view.Problem = problem
			render(writer, page, view, status)
			return
		}
		view.Title = "Distill " + projectOf(target.clone)
		view.Back = "/projects?project=" + projectOf(target.clone)
		view.Hidden = []formField{{Name: "folder", Value: target.clone.ClonePath}}
		view.Summary = []string{wording.Distill(shortPath(target.clone.ClonePath))}
		render(writer, page, view, http.StatusOK)
	})
}

// distillAction runs distill for a clone, as rein distill does in it, the
// distiller's progress on the run page, under the machine lock for the
// whole run as the CLI holds it (P4.4).
func distillAction(orchestrator orchestration.Orchestrator, version string, actions *runner, page *template.Template) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		target, problem, status, err := targetOf(orchestrator, request, false)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		name := "Distill " + projectOf(target.clone)
		begin(writer, request, actions, problem, status, name, func(current *run) (done outcome, err error) {
			acting := orchestrator
			acting.Progress = current.record
			release, err := lockFor(current, acting, "rein distill", target.clone.ClonePath)
			if err != nil {
				return joinedOutcome(err)
			}
			defer func() { release(err) }()
			result, err := acting.DistillHarness(orchestration.DistillRequest{Directory: target.clone.ClonePath})
			if err != nil {
				return outcome{}, err
			}
			distilled := wording.Distilled(result)
			return outcome{Headline: distilled.Headline, Lines: linesOf(append(distilled.Lines, distilled.Closing...)), Next: distilled.Next}, nil
		}, func() previewView {
			return previewView{frame: frameOf(request, target.report, version, name, "projects", actions)}
		}, page)
	})
}

// restoreOutcome is what reset project did, in the words the CLI prints.
func restoreOutcome(result orchestration.RestoreResult) outcome {
	done := wording.Restored(result)
	return outcome{Headline: done.Headline, Lines: linesOf(done.Lines), Next: done.Next}
}
