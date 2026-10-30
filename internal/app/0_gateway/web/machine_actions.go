package web

import (
	"html/template"
	"net/http"

	"github.com/Danidani23/agents-rein/internal/app/0_gateway/wording"
	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
)

// The machine's actions on This machine (P4.8), each through the pattern
// of P4.3 and in the words the CLI uses (D60).

// resetMachinePreview says what reset machine will do, line by line as the
// CLI prints it.
func resetMachinePreview(orchestrator orchestration.Orchestrator, version string, actions *runner, page *template.Template) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		report, err := orchestrator.Status()
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		plan, err := orchestrator.PlanReset()
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		view := previewView{frame: frameOf(request, report, version, "Reset this machine", "machine", actions), Action: "/actions/reset-machine", Confirm: "Reset this machine", Back: "/", Busy: actions.runningName()}
		view.Summary = []string{wording.ResetMachineExplanation}
		view.ItemsHeading = "What it does"
		view.Items = wording.ResetMachine(plan)[1:]
		render(writer, page, view, http.StatusOK)
	})
}

// resetMachineAction runs reset machine, under the machine lock as any
// change.
func resetMachineAction(orchestrator orchestration.Orchestrator, version string, actions *runner, page *template.Template) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		report, err := orchestrator.Status()
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		begin(writer, request, actions, "", 0, "Reset this machine", func(current *run) (done outcome, err error) {
			acting := orchestrator
			acting.Progress = current.record
			release, err := lockFor(current, acting, "rein reset machine", "")
			if err != nil {
				return joinedOutcome(err)
			}
			defer func() { release(err) }()
			plan, err := acting.PlanReset()
			if err != nil {
				return outcome{}, err
			}
			result, err := acting.ResetMachine(plan)
			var lines []outcomeLine
			for _, project := range result.ProjectClones {
				if project.Skipped != "" {
					lines = append(lines, linesOf([]wording.Line{wording.ProjectReset(project)})...)
					continue
				}
				restored := wording.Restored(project.Restore)
				lines = append(lines, outcomeLine{Text: restored.Headline})
				lines = append(lines, linesOf(restored.Lines)...)
			}
			if err != nil {
				return outcome{Lines: lines}, err
			}
			reset := wording.MachineReset(result)
			return outcome{Headline: reset.Headline, Lines: append(lines, linesOf(reset.Lines)...), Next: reset.Next}, nil
		}, func() previewView {
			return previewView{frame: frameOf(request, report, version, "Reset this machine", "machine", actions)}
		}, page)
	})
}

// updatePreview says where this rein stands and what an update would do,
// as rein update does. Only rein ui in a browser offers it: the app's
// window is a build of its own, which a release of rein does not replace.
func updatePreview(orchestrator orchestration.Orchestrator, version string, actions *runner, page *template.Template) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		report, err := orchestrator.Status()
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		view := previewView{frame: frameOf(request, report, version, "Update rein", "machine", actions), Action: "/actions/update", Confirm: "Update rein", Back: "/", Busy: actions.runningName()}
		plan, err := orchestrator.PlanUpdate()
		if err != nil {
			view.Problem = err.Error()
			render(writer, page, view, http.StatusBadGateway)
			return
		}
		lines, summary, explanation, refused := wording.Update(plan)
		view.Summary = lines
		switch {
		case refused != "":
			view.Problem = refused
		case summary == "":
			view.Problem = "there is nothing rein updates here; " + lines[len(lines)-1]
		default:
			view.Summary = append(view.Summary, summary, explanation)
		}
		render(writer, page, view, http.StatusOK)
	})
}

// updateAction puts the latest release in place of this rein.
func updateAction(orchestrator orchestration.Orchestrator, version string, actions *runner, page *template.Template) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		report, err := orchestrator.Status()
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		begin(writer, request, actions, "", 0, "Update rein", func(current *run) (outcome, error) {
			replaced, released, err := orchestrator.Update()
			if err != nil {
				return outcome{}, err
			}
			return outcome{Headline: wording.Updated(replaced, released), Next: "stop rein ui with Ctrl-C in its terminal and start it again: this page is served by the rein it replaced"}, nil
		}, func() previewView {
			return previewView{frame: frameOf(request, report, version, "Update rein", "machine", actions)}
		}, page)
	})
}

// keyRemovePreview says what removing a provider's key does.
func keyRemovePreview(orchestrator orchestration.Orchestrator, version string, actions *runner, page *template.Template) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		report, err := orchestrator.Status()
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		name := request.FormValue("name")
		view := previewView{frame: frameOf(request, report, version, "Remove "+name, "machine", actions), Action: "/actions/key-remove", Confirm: "Remove the key", Back: "/", Busy: actions.runningName()}
		if _, found := orchestrator.Credentials.Key(name); !found {
			view.Problem = name + " is not in your keychain"
			render(writer, page, view, http.StatusNotFound)
			return
		}
		view.Hidden = []formField{{Name: "name", Value: name}}
		view.Summary = []string{wording.KeyRemove(name)}
		render(writer, page, view, http.StatusOK)
	})
}

// keyRemoveAction removes a provider's key, as rein key remove does.
func keyRemoveAction(orchestrator orchestration.Orchestrator, version string, actions *runner, page *template.Template) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		report, err := orchestrator.Status()
		if err != nil {
			http.Error(writer, err.Error(), http.StatusInternalServerError)
			return
		}
		name := request.FormValue("name")
		begin(writer, request, actions, "", 0, "Remove "+name, func(current *run) (outcome, error) {
			err := orchestrator.Credentials.DeleteKey(name)
			if err != nil {
				return outcome{}, err
			}
			return outcome{Headline: wording.KeyRemoved(name)}, nil
		}, func() previewView {
			return previewView{frame: frameOf(request, report, version, "Remove "+name, "machine", actions)}
		}, page)
	})
}
