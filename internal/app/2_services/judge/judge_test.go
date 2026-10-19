package judge

import (
	"strings"
	"testing"

	"github.com/Danidani23/agents-rein/internal/adapters/fake"
	"github.com/Danidani23/agents-rein/internal/app/2_services/distill"
)

func candidates() []distill.Candidate {
	return []distill.Candidate{{Kind: "rules", ID: "brief", Files: map[string]string{"rules/brief.md": "Be brief."}}}
}

const good = `{"version":1,"candidates":[{"candidate":"rules/brief","verdict":"new","suggestion":"keep","reason":"nothing like it"}]}`

func TestJudgeCandidates(t *testing.T) {
	t.Run("a believed answer is applied", func(t *testing.T) {
		backend := &fake.Judge{Answers: []string{good}}
		items := candidates()
		run := Service{Judge: backend}.JudgeCandidates("organizations.acme", items, distill.Corpus{})
		if run.Status != distill.JudgeAnswered || run.Backend != "fake-judge" || items[0].Judgement == nil || items[0].Judgement.Reason != "nothing like it" || len(backend.Briefs) != 1 {
			t.Fatalf("run %+v judgement %+v", run, items[0].Judgement)
		}
	})
	t.Run("a bad answer goes back once with the problems, then counts as failed", func(t *testing.T) {
		backend := &fake.Judge{Answers: []string{"nonsense", good}}
		items := candidates()
		run := Service{Judge: backend}.JudgeCandidates("organizations.acme", items, distill.Corpus{})
		if run.Status != distill.JudgeAnswered || len(backend.Briefs) != 2 || !strings.Contains(backend.Briefs[1], "previous answer was not accepted") || !strings.Contains(backend.Briefs[1], "holds no JSON object") {
			t.Fatalf("run %+v briefs %d", run, len(backend.Briefs))
		}
		backend = &fake.Judge{Answers: []string{"nonsense", `{"version":1,"candidates":[]}`}}
		items = candidates()
		run = Service{Judge: backend}.JudgeCandidates("organizations.acme", items, distill.Corpus{})
		if run.Status != distill.JudgeFailed || !strings.Contains(strings.Join(run.Problems, ";"), "rules/brief is not answered") || items[0].Judgement != nil {
			t.Fatalf("run %+v", run)
		}
	})
	t.Run("a backend error and an unavailable backend", func(t *testing.T) {
		run := Service{Judge: &fake.Judge{}}.JudgeCandidates("organizations.acme", candidates(), distill.Corpus{})
		if run.Status != distill.JudgeFailed || !strings.Contains(run.Problems[0], "no answer left") {
			t.Fatalf("run %+v", run)
		}
		reason, ok := Service{Judge: &fake.Judge{Unavailable: "no claude"}}.Available()
		if ok || reason != "no claude" {
			t.Fatalf("available %v %q", ok, reason)
		}
		reason, ok = Service{}.Available()
		if ok || !strings.Contains(reason, "no judge backend") {
			t.Fatalf("available %v %q", ok, reason)
		}
	})
}
