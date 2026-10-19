// Package judge is the service that asks the model: it holds the backend
// the organisation's provider chose, or the agent's CLI as the fallback
// (D23), and asks once more with the validation problems when an answer is
// not believed. It owns no state.
package judge

import (
	"fmt"
	"strings"

	"github.com/Danidani23/agents-rein/internal/app/2_services/distill"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// Service asks the judge.
type Service struct {
	Judge ports.Judge
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

// JudgeCandidates sends the brief, validates the answer, sends the problems
// back once, and applies the judgements it believes. The run says what
// happened; a failed run leaves the candidates as the mechanical pass had
// them.
func (service Service) JudgeCandidates(level string, candidates []distill.Candidate, corpus distill.Corpus) distill.JudgeRun {
	run := distill.JudgeRun{Backend: service.Name(), Dropped: corpus.Dropped}
	brief := distill.Brief(level, candidates, corpus)
	for attempt := 0; attempt < 2; attempt++ {
		text, err := service.Judge.Ask(brief)
		if err != nil {
			run.Status = distill.JudgeFailed
			run.Problems = append(run.Problems, err.Error())
			return run
		}
		answer, err := distill.ParseAnswer(text)
		var problems []string
		if err != nil {
			problems = []string{err.Error()}
		} else {
			problems = distill.Validate(answer, candidates, corpus)
		}
		if len(problems) == 0 {
			distill.Apply(answer, candidates)
			run.Status = distill.JudgeAnswered
			return run
		}
		run.Problems = problems
		brief = brief + fmt.Sprintf("\n\n# Your previous answer was not accepted\n\nFix these and answer again with JSON only:\n- %s\n", strings.Join(problems, "\n- "))
	}
	run.Status = distill.JudgeFailed
	return run
}
