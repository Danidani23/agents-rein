package web

import (
	"fmt"
	"html/template"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
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

// lockWait is how long an action waits for another rein to finish, as the
// CLI's writing commands wait.
const lockWait = 5 * time.Second

// lockFor takes the machine lock for one action; a lock another rein holds
// is a refusal, nothing changed.
func lockFor(orchestrator orchestration.Orchestrator) (release func(), err error) {
	if orchestrator.Lock == nil {
		return func() {}, nil
	}
	release, err = orchestrator.Lock.Acquire(lockWait)
	if err != nil {
		return nil, refusal.New(err.Error())
	}
	return release, nil
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
		return target, "not logged in; run rein login in a terminal, it opens the GitHub device flow", http.StatusForbidden, nil
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
		begin(writer, request, actions, problem, status, name, func(progress func(orchestration.ProgressEvent)) (outcome, error) {
			acting := orchestrator
			acting.Progress = progress
			release, err := lockFor(acting)
			if err != nil {
				return outcome{}, err
			}
			defer release()
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
		view.Summary = []string{
			fmt.Sprintf("This will put back the agent's files rein init moved out of %s, take the rein block out of .gitignore, forget the clone on this machine, and commit the put-back.", shortPath(target.clone.ClonePath)),
			"The files come back from ~/.rein/snapshots/ exactly as they were. The commit holds only the files that were tracked and .gitignore; pushing it is yours. The snapshots stay until rein reset machine.",
		}
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
		begin(writer, request, actions, problem, status, name, func(progress func(orchestration.ProgressEvent)) (outcome, error) {
			acting := orchestrator
			acting.Progress = progress
			release, err := lockFor(acting)
			if err != nil {
				return outcome{}, err
			}
			defer release()
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

// syncOutcome is what sync did to each clone, worded as the CLI prints it.
func syncOutcome(result orchestration.SyncResult) outcome {
	done := outcome{Headline: "done", Next: "a new session in the clone reads the harness, and /clear loads it in an open one; a change to it is proposed with rein distill and rein propose, never edited in the clone"}
	for _, synced := range result.Clones {
		if synced.Skipped != "" {
			done.Lines = append(done.Lines, outcomeLine{Text: "skipped " + shortPath(synced.Folder) + ": " + synced.Skipped, Warning: true})
			continue
		}
		done.Lines = append(done.Lines, outcomeLine{Text: fmt.Sprintf("synced %s for %s: %d written, %d unchanged, %d removed", synced.Address, synced.Agent, len(synced.Written), len(synced.Unchanged), len(synced.Removed))})
		if synced.SnapshotPath != "" && len(synced.Modified) > 0 {
			done.Lines = append(done.Lines, outcomeLine{Text: fmt.Sprintf("your edits to %s were snapshotted to %s before being overwritten", strings.Join(synced.Modified, ", "), shortPath(synced.SnapshotPath))})
		}
		if len(synced.Foreign) > 0 {
			done.Lines = append(done.Lines, outcomeLine{Text: "not written by rein, so in no other clone and not in the organisation's repository: " + strings.Join(synced.Foreign, ", "), Warning: true})
		}
		for _, note := range synced.Notes {
			done.Lines = append(done.Lines, outcomeLine{Text: "note: " + note})
		}
		if len(synced.UnsetVariables) > 0 {
			done.Lines = append(done.Lines, outcomeLine{Text: fmt.Sprintf("the tool servers of %s need %s in your shell, not set", synced.Address, strings.Join(synced.UnsetVariables, ", ")), Warning: true})
		}
		for _, untrusted := range synced.Untrusted {
			done.Lines = append(done.Lines, outcomeLine{Text: fmt.Sprintf("%s has not trusted this folder yet, so rein's tool server stays pending; start it there once and accept the trust dialog: cd %s && %s", untrusted.Agent, synced.Folder, untrusted.Command), Warning: true})
		}
	}
	return done
}

// restoreOutcome is what reset project did, worded as the CLI prints it.
func restoreOutcome(result orchestration.RestoreResult) outcome {
	done := outcome{Headline: fmt.Sprintf("restored %s, formerly %s", shortPath(result.Folder), result.Address), Next: "the clone is as before rein init; rein init puts it under rein again, rein org destroy and rein reset machine take the rest off this machine"}
	if len(result.Restored) > 0 {
		done.Lines = append(done.Lines, outcomeLine{Text: "put back: " + strings.Join(result.Restored, ", ")})
	} else {
		done.Lines = append(done.Lines, outcomeLine{Text: "nothing to put back: init had moved nothing out"})
	}
	if len(result.InsertsRemoved) > 0 {
		done.Lines = append(done.Lines, outcomeLine{Text: "removed sync's files: " + strings.Join(result.InsertsRemoved, ", ")})
	}
	if len(result.InsertsKept) > 0 {
		done.Lines = append(done.Lines, outcomeLine{Text: "kept, edited by hand since sync wrote them: " + strings.Join(result.InsertsKept, ", "), Warning: true})
	}
	if result.IgnoreRemoved {
		done.Lines = append(done.Lines, outcomeLine{Text: ".gitignore: rein block removed"})
	}
	switch {
	case result.Commit != "":
		done.Lines = append(done.Lines, outcomeLine{Text: "committed " + result.Commit[:min(7, len(result.Commit))] + ": restore agent settings from rein (rein reset project)"})
	case result.CommitError != "":
		done.Lines = append(done.Lines, outcomeLine{Text: "the put-back is staged but not committed: " + result.CommitError, Warning: true})
	default:
		done.Lines = append(done.Lines, outcomeLine{Text: "the put-back is staged, not committed"})
	}
	return done
}
