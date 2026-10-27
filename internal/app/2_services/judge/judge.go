// Package judge is the service that asks the model: it holds the backend
// the organisation's provider chose, or the agent's CLI as the fallback
// (D23), and sends the validation problems back into the conversation once
// when an answer is not believed (P2.58). It owns no state.
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
// back once into the same conversation, and returns the candidates with the
// judgements it believes, the units cut out in place of the candidates they
// came from. The run says what happened, with the problems an answer was
// sent back for; a failed run returns the candidates as the mechanical pass
// had them. report, which may be nil, hears in a few words how far the
// answer has come (P2.54).
func (service Service) JudgeCandidates(level string, candidates []distill.Candidate, corpus distill.Corpus, model string, report func(note string)) (distill.JudgeRun, []distill.Candidate) {
	run := distill.JudgeRun{Backend: service.Name(), Dropped: corpus.Dropped}
	// the first line names the conversation in the agent's history
	brief := fmt.Sprintf("rein distill, the distiller's brief for %s, %s\n\n", level, time.Now().Format("2006-01-02 15:04")) + distill.Brief(level, candidates, corpus)
	text, session, err := service.Judge.Ask(brief, model, progressNotes(report))
	run.Session = session
	if err != nil {
		run.Status = distill.JudgeFailed
		run.Problems = []string{err.Error()}
		return run, candidates
	}
	answer, problems := believe(text, candidates, corpus)
	if len(problems) == 0 {
		run.Status = distill.JudgeAnswered
		return run, distill.Apply(answer, candidates)
	}
	// the distiller mends its own answer rather than judging anew, so what it got right stays (P2.58)
	run.Rejected = problems
	if report != nil {
		report("its first answer was not accepted; asking again with the problems")
	}
	conversation := ports.JudgeConversation{Brief: brief, Answer: text, Session: session}
	mending := fmt.Sprintf("# Your previous answer was not accepted\n\nFix these problems and answer again with the whole answer, JSON only. Change only what they need and keep every other judgement and unit as it was:\n- %s\n", strings.Join(problems, "\n- "))
	text, session, err = service.Judge.Reply(conversation, mending, model, progressNotes(report))
	run.Session = session
	if err != nil {
		run.Status = distill.JudgeFailed
		run.Problems = []string{err.Error()}
		return run, candidates
	}
	answer, problems = believe(text, candidates, corpus)
	if len(problems) == 0 {
		run.Status = distill.JudgeAnswered
		return run, distill.Apply(answer, candidates)
	}
	run.Status = distill.JudgeFailed
	run.Problems = problems
	return run, candidates
}

// believe parses an answer and validates it, returning what is wrong with
// it, none when it is believed.
func believe(text string, candidates []distill.Candidate, corpus distill.Corpus) (distill.Answer, []string) {
	answer, err := distill.ParseAnswer(text)
	if err != nil {
		return distill.Answer{}, []string{err.Error()}
	}
	return answer, distill.Validate(answer, candidates, corpus)
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
