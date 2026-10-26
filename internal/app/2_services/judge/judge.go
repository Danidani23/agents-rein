// Package judge is the service that asks the model: it holds the backend
// the organisation's provider chose, or the agent's CLI as the fallback
// (D23), and asks once more with the validation problems when an answer is
// not believed. It owns no state.
package judge

import (
	"fmt"
	"strings"
	"time"

	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/distill"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// Service asks the judge.
type Service struct {
	Judge ports.Judge
	// DefaultModel is the model the backend asks when none is named, ""
	// for the developer's own default.
	DefaultModel string
	// Provider builds the backend for an organisation's provider (D42);
	// nil where none can be built.
	Provider func(provider declaration.Provider) ports.Judge
}

// WithProvider is the service asking the organisation's provider instead,
// its model the default.
func (service Service) WithProvider(provider declaration.Provider) Service {
	return Service{Judge: service.Provider(provider), DefaultModel: provider.Model, Provider: service.Provider}
}

// Available says whether a judge can answer on this machine.
func (service Service) Available() (reason string, ok bool) {
	if service.Judge == nil {
		return "no judge backend is wired", false
	}
	return service.Judge.Available()
}

// Name is the backend's name.
func (service Service) Name() string {
	if service.Judge == nil {
		return "none"
	}
	return service.Judge.Name()
}

// Describe names who answers, for a person: the backend without its kind,
// and the model, the provider's, or the developer's default.
func (service Service) Describe(model string) string {
	switch {
	case model == "" && service.DefaultModel != "":
		model = service.DefaultModel
	case model == "":
		model = "your default model"
	}
	return strings.TrimPrefix(strings.TrimPrefix(service.Name(), "agent-cli:"), "api:") + " with " + model
}

// ResumeCommand is the command that opens the conversation of a run, ""
// when there is none.
func (service Service) ResumeCommand(session string) string {
	if service.Judge == nil || session == "" {
		return ""
	}
	return service.Judge.ResumeCommand(session)
}

// JudgeCandidates sends the brief, validates the answer, sends the problems
// back once, and returns the candidates with the judgements it believes,
// the units cut out in place of the candidates they came from. The run says
// what happened; a failed run returns the candidates as the mechanical pass
// had them. report, which may be nil, hears in a few words how far the
// answer has come (P2.54).
func (service Service) JudgeCandidates(level string, candidates []distill.Candidate, corpus distill.Corpus, model string, report func(note string)) (distill.JudgeRun, []distill.Candidate) {
	run := distill.JudgeRun{Backend: service.Name(), Dropped: corpus.Dropped}
	// the first line names the conversation in the agent's history
	brief := fmt.Sprintf("rein distill, the distiller's brief for %s, %s\n\n", level, time.Now().Format("2006-01-02 15:04")) + distill.Brief(level, candidates, corpus)
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 && report != nil {
			report("its first answer was not accepted; asking again with the problems")
		}
		text, session, err := service.Judge.Ask(brief, model, progressNotes(report))
		run.Session = session
		if err != nil {
			run.Status = distill.JudgeFailed
			run.Problems = append(run.Problems, err.Error())
			return run, candidates
		}
		answer, err := distill.ParseAnswer(text)
		var problems []string
		if err != nil {
			problems = []string{err.Error()}
		} else {
			problems = distill.Validate(answer, candidates, corpus)
		}
		if len(problems) == 0 {
			run.Status = distill.JudgeAnswered
			// a retried answer that is believed leaves the first one's problems behind
			run.Problems = nil
			return run, distill.Apply(answer, candidates)
		}
		run.Problems = problems
		brief = brief + fmt.Sprintf("\n\n# Your previous answer was not accepted\n\nFix these and answer again with JSON only:\n- %s\n", strings.Join(problems, "\n- "))
	}
	run.Status = distill.JudgeFailed
	return run, candidates
}

// progressNotes turns an answer's progress into notes for a person, each
// said once: thinking, then how many judgements the answer holds so far.
func progressNotes(report func(note string)) func(ports.JudgeProgress) {
	if report == nil {
		return nil
	}
	said := ""
	return func(update ports.JudgeProgress) {
		note := ""
		switch {
		case update.Written != "":
			note = "writing its answer"
			judged := strings.Count(update.Written, `"verdict"`)
			if judged == 1 {
				note += ", 1 judgement so far"
			} else if judged > 1 {
				note += fmt.Sprintf(", %d judgements so far", judged)
			}
		case update.Thinking:
			note = "thinking"
		}
		if note != "" && note != said {
			said = note
			report(note)
		}
	}
}
