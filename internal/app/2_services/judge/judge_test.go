package judge

import (
	"strings"
	"testing"

	"github.com/Danidani23/agents-rein/internal/adapters/fake"
	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/distill"
	"github.com/Danidani23/agents-rein/internal/ports"
)

func candidates() []distill.Candidate {
	return []distill.Candidate{{Kind: "rules", ID: "brief", Files: map[string]string{"rules/brief.md": "Be brief."}}}
}

const good = `{"version":2,"candidates":[{"candidate":"rules/brief","verdict":"new","suggestion":"keep","reason":"nothing like it"}]}`

func TestJudgeCandidates(t *testing.T) {
	t.Run("a believed answer is applied", func(t *testing.T) {
		backend := &fake.Judge{Answers: []string{good}}
		run, items := Service{Judge: backend}.JudgeCandidates("organizations.acme", candidates(), distill.Corpus{}, "", nil)
		if run.Status != distill.JudgeAnswered || run.Backend != "fake-judge" || items[0].Judgement == nil || items[0].Judgement.Reason != "nothing like it" || len(backend.Briefs) != 1 {
			t.Fatalf("run %+v judgement %+v", run, items[0].Judgement)
		}
	})
	t.Run("a bad answer goes back once into its conversation with the problems, then counts as failed (P2.58)", func(t *testing.T) {
		backend := &fake.Judge{Answers: []string{"nonsense", good}}
		run, _ := Service{Judge: backend}.JudgeCandidates("organizations.acme", candidates(), distill.Corpus{}, "", nil)
		if run.Status != distill.JudgeAnswered || len(backend.Briefs) != 1 || len(backend.Replies) != 1 {
			t.Fatalf("run %+v briefs %d replies %d", run, len(backend.Briefs), len(backend.Replies))
		}
		// the distiller mends its own answer, so the judgements it got right stay as they were
		reply := backend.Replies[0]
		if reply.Conversation.Brief != backend.Briefs[0] || reply.Conversation.Answer != "nonsense" || reply.Conversation.Session != "fake-session" {
			t.Fatalf("conversation %+v", reply.Conversation)
		}
		if !strings.Contains(reply.Message, "previous answer was not accepted") || !strings.Contains(reply.Message, "holds no JSON object") || !strings.Contains(reply.Message, "keep every other judgement") || strings.Contains(reply.Message, "You are the distiller") {
			t.Fatalf("message %q", reply.Message)
		}
		// the accepted answer has no problems; the ones the first was sent back for stay on the run
		if len(run.Problems) != 0 || strings.Join(run.Rejected, ";") != "the answer holds no JSON object" || run.Session != "fake-session" {
			t.Fatalf("an accepted retry reports problems %v rejected %v session %q", run.Problems, run.Rejected, run.Session)
		}
		backend = &fake.Judge{Answers: []string{"nonsense", `{"version":2,"candidates":[]}`}}
		run, items := Service{Judge: backend}.JudgeCandidates("organizations.acme", candidates(), distill.Corpus{}, "", nil)
		if run.Status != distill.JudgeFailed || !strings.Contains(strings.Join(run.Problems, ";"), "rules/brief is not answered") || strings.Join(run.Rejected, ";") != "the answer holds no JSON object" || items[0].Judgement != nil {
			t.Fatalf("run %+v", run)
		}
		// a first answer that is believed is sent back for nothing
		backend = &fake.Judge{Answers: []string{good}}
		run, _ = Service{Judge: backend}.JudgeCandidates("organizations.acme", candidates(), distill.Corpus{}, "", nil)
		if len(run.Rejected) != 0 || len(backend.Replies) != 0 {
			t.Fatalf("run %+v replies %d", run, len(backend.Replies))
		}
	})
	t.Run("its progress is said as it comes: thinking, then the judgements written (P2.54)", func(t *testing.T) {
		backend := &fake.Judge{Answers: []string{"nonsense", good}, Progress: []ports.JudgeProgress{{Thinking: true}, {Written: `{"candidates":[{"verdict":"new"},{"verdict":"new"}`}}}
		var notes []string
		_, _ = Service{Judge: backend}.JudgeCandidates("organizations.acme", candidates(), distill.Corpus{}, "", func(note string) { notes = append(notes, note) })
		if strings.Join(notes, "|") != "thinking|writing its answer, 2 judgements so far|its first answer was not accepted; asking again with the problems|thinking|writing its answer, 2 judgements so far" {
			t.Fatalf("notes %q", notes)
		}
		if (Service{Judge: backend}).Describe("") != "fake-judge with your default model" || (Service{Judge: backend}).Describe("sonnet") != "fake-judge with sonnet" {
			t.Fatalf("describe %q", (Service{Judge: backend}).Describe(""))
		}
	})
	t.Run("an organisation's provider replaces the backend, its model the default (D42)", func(t *testing.T) {
		provided := &fake.Judge{}
		var declared declaration.Provider
		service := Service{Judge: &fake.Judge{}, Provider: func(provider declaration.Provider) ports.Judge {
			declared = provider
			return provided
		}}
		switched := service.WithProvider(declaration.Provider{Model: "deepseek-v4-pro"})
		if switched.Judge != provided || declared.Model != "deepseek-v4-pro" || switched.Describe("") != "fake-judge with deepseek-v4-pro" || switched.Describe("other") != "fake-judge with other" {
			t.Fatalf("switched %+v describe %q", switched, switched.Describe(""))
		}
	})
	t.Run("a backend error and an unavailable backend", func(t *testing.T) {
		run, _ := Service{Judge: &fake.Judge{}}.JudgeCandidates("organizations.acme", candidates(), distill.Corpus{}, "", nil)
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
