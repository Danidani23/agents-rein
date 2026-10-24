package distill

import (
	"strings"
	"testing"
)

func exampleCandidates() []Candidate {
	return []Candidate{
		{Kind: "rules", ID: "go-testing", Files: map[string]string{"rules/go-testing.md": "Use table-driven tests only.\n"}},
		{Kind: "rules", ID: "brief", Files: map[string]string{"rules/brief.md": "Be brief.\n"}},
	}
}

func exampleCorpus() Corpus {
	return Corpus{Passages: []Passage{
		{Address: "organizations.acme.teams.platform.rules.go-style", Level: "organizations.acme.teams.platform", Kind: "rules", Enforcement: "required", ApplyIf: []string{"language:go"}, Text: "Go style.\n\nTests are table-driven tests only, never ad hoc.", Resolved: true},
		{Address: "organizations.acme.teams.data.rules.notebooks", Level: "organizations.acme.teams.data", Kind: "rules", Text: "Notebooks stay out of main.", Resolved: false},
	}}
}

const goodAnswer = `Here you go:
` + "```json" + `
{"version": 1, "candidates": [
  {"candidate": "rules/go-testing", "verdict": "overlaps", "related": ["organizations.acme.teams.platform.rules.go-style"], "quotes": [{"address": "organizations.acme.teams.platform.rules.go-style", "text": "table-driven   tests only"}], "suggestion": "drop", "reason": "the team rule already says it"},
  {"candidate": "rules/brief", "verdict": "new", "suggestion": "lift", "level": "organization", "applyIf": [], "reason": "every project benefits"}
]}
` + "```"

func TestBrief(t *testing.T) {
	brief := Brief("organizations.acme.projects.shop", exampleCandidates(), exampleCorpus())
	for _, expected := range []string{"## rules/go-testing", "Use table-driven tests only.", "## organizations.acme.teams.platform.rules.go-style (applies, required, applyIf language:go)", "## organizations.acme.teams.data.rules.notebooks (other team)", `"version": 1`, "Answer with JSON only"} {
		if !strings.Contains(brief, expected) {
			t.Fatalf("brief lacks %q:\n%s", expected, brief)
		}
	}
	// the distiller selects; it never writes a setting's text, so there is no merge
	if strings.Contains(brief, "merge") || strings.Contains(brief, "rewrite") {
		t.Fatalf("the brief still offers a merge:\n%s", brief)
	}
}

func TestParseValidateApply(t *testing.T) {
	answer, err := ParseAnswer(goodAnswer)
	if err != nil || len(answer.Candidates) != 2 {
		t.Fatalf("answer %+v err %v", answer, err)
	}
	candidates := exampleCandidates()
	problems := Validate(answer, candidates, exampleCorpus())
	if len(problems) != 0 {
		t.Fatalf("problems %v", problems)
	}
	Apply(answer, candidates)
	if candidates[0].Judgement == nil || candidates[0].Judgement.Suggestion != "drop" || candidates[1].Judgement.Level != "organization" {
		t.Fatalf("judgements %+v %+v", candidates[0].Judgement, candidates[1].Judgement)
	}
	_, err = ParseAnswer("no json here")
	if err == nil {
		t.Fatalf("prose parsed")
	}
	bad := Answer{Version: 2, Candidates: []AnswerCandidate{
		{Candidate: "rules/go-testing", Judgement: Judgement{Verdict: "maybe", Related: []string{"organizations.acme.rules.nope"}, Quotes: []Quote{{Address: "organizations.acme.teams.platform.rules.go-style", Text: "never written there"}}, Suggestion: "lift", Reason: ""}},
		{Candidate: "rules/unknown", Judgement: Judgement{Verdict: "new", Suggestion: "keep", Reason: "x"}},
		{Candidate: "rules/brief", Judgement: Judgement{Verdict: "new", Suggestion: "merge", Reason: "x"}},
	}}
	problems = Validate(bad, candidates, exampleCorpus())
	joined := strings.Join(problems, "\n")
	for _, expected := range []string{"version 2", `verdict "maybe"`, `related "organizations.acme.rules.nope"`, "is not verbatim", "lift needs a level", "no reason", `"rules/unknown" is not a candidate`, `suggestion "merge" is not one of keep, drop, lift`} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("missing %q in %v", expected, problems)
		}
	}
}

func TestCorpusTrim(t *testing.T) {
	big := strings.Repeat("x", corpusBudget/2+1)
	corpus := Corpus{Passages: []Passage{
		{Address: "a", Text: big, Resolved: true},
		{Address: "b", Text: big, Resolved: false},
		{Address: "c", Text: "small", Resolved: true},
	}}
	corpus.trim()
	if len(corpus.Passages) != 2 || strings.Join(corpus.Dropped, ",") != "b" {
		t.Fatalf("passages %d dropped %v", len(corpus.Passages), corpus.Dropped)
	}
	corpus = Corpus{Passages: []Passage{{Address: "a", Text: big, Resolved: true}, {Address: "d", Text: big + "x", Resolved: true}}}
	corpus.trim()
	if strings.Join(corpus.Dropped, ",") != "d" {
		t.Fatalf("dropped %v", corpus.Dropped)
	}
}
