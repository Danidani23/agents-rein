package proposal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/distill"
)

func sample() Proposal {
	when := time.Date(2026, 10, 22, 16, 22, 13, 0, time.UTC)
	skill := distill.Candidate{Kind: "skills", ID: "go-style", Sources: []string{".claude/skills/go-style/SKILL.md"}, Verdict: "new", Files: map[string]string{"skills/go-style/SKILL.md": "---\nname: go-style\ndescription: Go style\n---\n\n# Go\n", "skills/go-style/references/errors.md": "two lines\n"}}
	rule := distill.Candidate{Kind: "rules", ID: "docs", Sources: []string{".claude/rules/docs.md"}, Verdict: "new", Files: map[string]string{"rules/docs.md": "---\npaths:\n  - docs/**\n---\n\nCite sources.\n"}}
	return Proposal{
		Version: Version, ID: "3f9c2e1a7b4d", Status: StatusPending, Organization: "acme", DistilledFrom: "organizations.acme.projects.shop",
		Snapshot:    Snapshot{Path: "/snapshots/shop.json", SHA256: "3f9c2e1a7b4d0000", TakenAt: when},
		DistilledAt: when, Judge: distill.JudgeRun{Backend: "fake", Status: "answered"}, Skipped: []string{},
		Candidates: []Candidate{
			FromCandidate(skill, "organizations.acme", true, []string{"language:go"}, &Judgement{Verdict: "new", Suggestion: "lift", Level: "organizations.acme", ApplyIf: []string{"language:go"}, Reason: "general"}),
			FromCandidate(rule, "organizations.acme.projects.shop", false, nil, &Judgement{Verdict: "duplicate", Suggestion: "drop", Reason: "covered"}),
		},
	}
}

func TestProposalRoundTrip(t *testing.T) {
	document := sample()
	folder := filepath.Join(t.TempDir(), FolderName(document.ID))
	err := document.WriteFolder(folder)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	index, _ := os.ReadFile(filepath.Join(folder, IndexFile))
	text := string(index)
	for _, expected := range []string{Header, "version: 2\n", "id: 3f9c2e1a7b4d\n", "status: pending\n", "pull_request: \"\"\n", "      include: true\n      level: organizations.acme\n      applyIf:\n        - language:go\n      doNotApplyIf: []\n      enforcement: \"\"\n", "      entry: skill_go-style\n", "      entry: rule_docs.md\n", "      include: false\n"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("missing %q in %s", expected, text)
		}
	}
	// the content lives beside the index, one entry per candidate
	if strings.Contains(text, "files:") || strings.Contains(text, "proposed_at") {
		t.Fatalf("the index holds content or a time that has not happened: %s", text)
	}
	reference, _ := os.ReadFile(filepath.Join(folder, "skill_go-style", "references", "errors.md"))
	rule, _ := os.ReadFile(filepath.Join(folder, "rule_docs.md"))
	if string(reference) != "two lines\n" || !strings.HasPrefix(string(rule), "---\npaths:") {
		t.Fatalf("entries: %q %q", reference, rule)
	}
	read, err := Load(folder)
	if err != nil || read.ID != document.ID || len(read.Candidates) != 2 || read.Candidates[0].Files["references/errors.md"] != "two lines\n" || read.Candidates[0].Judge.Level != "organizations.acme" || !read.Snapshot.TakenAt.Equal(document.Snapshot.TakenAt) {
		t.Fatalf("read %+v err %v", read, err)
	}
	// an edit in an entry is what propose reads
	_ = os.WriteFile(filepath.Join(folder, "rule_docs.md"), []byte("Cite every source.\n"), 0o600)
	read, _ = Load(folder)
	if read.Candidates[1].Files["docs.md"] != "Cite every source.\n" {
		t.Fatalf("the edit was not read: %+v", read.Candidates[1].Files)
	}
	// an entry outside the folder is refused
	_ = os.WriteFile(filepath.Join(folder, IndexFile), []byte(strings.Replace(text, "entry: rule_docs.md", "entry: ../secret.md", 1)), 0o600)
	_, err = Load(folder)
	if err == nil || !strings.Contains(err.Error(), "../secret.md") {
		t.Fatalf("an entry outside the folder: %v", err)
	}
	// a proposal written before folders, one YAML file, still reads
	legacy := sample()
	legacy.Version = FileVersion
	encoded, _ := yaml.Marshal(legacy)
	legacyPath := filepath.Join(t.TempDir(), "proposal-"+legacy.ID+".yaml")
	_ = os.WriteFile(legacyPath, encoded, 0o600)
	read, err = Load(legacyPath)
	if err != nil || read.Candidates[0].Files["references/errors.md"] != "two lines\n" {
		t.Fatalf("legacy %+v err %v", read, err)
	}
	_ = os.WriteFile(filepath.Join(folder, IndexFile), []byte("version: 3\n"), 0o600)
	_, err = Load(folder)
	if err == nil || !strings.Contains(err.Error(), "version 3") {
		t.Fatalf("version: %v", err)
	}
	if IDOf([]byte("a"), []byte("b")) != IDOf([]byte("ab")) || len(IDOf([]byte("x"))) != 12 || FolderName("abc") != "proposal-abc" || HistoryName("abc", document.DistilledAt) != "20261022-162213-abc" {
		t.Fatalf("names: %s %s", IDOf([]byte("x")), HistoryName("abc", document.DistilledAt))
	}
}

func TestProposalCheckAndFiles(t *testing.T) {
	registry := map[string]declaration.LabelCategory{"language": {Category: "language", Multiple: true, Labels: map[string]string{"go": "Go"}}, "data": {Category: "data", Labels: map[string]string{"public": "", "internal": ""}}}
	document := sample()
	if problems := document.Check(registry); len(problems) != 0 {
		t.Fatalf("problems %v", problems)
	}
	levels, byLevel := document.Included()
	if strings.Join(levels, ",") != "organizations.acme" || len(byLevel["organizations.acme"]) != 1 || len(document.Excluded()) != 1 {
		t.Fatalf("included %v excluded %v", levels, document.Excluded())
	}
	files, err := FilesOf(document.Candidates[0])
	parsed, skillProblems, _ := declaration.ParseSkill("SKILL.md", "go-style", []byte(files["skills/go-style/SKILL.md"]))
	if err != nil || len(skillProblems) != 0 || parsed.Metadata[declaration.SkillApplyIfKey] != "language:go" || files["skills/go-style/references/errors.md"] != "two lines\n" {
		t.Fatalf("files %v problems %v err %v", files, skillProblems, err)
	}
	rule := document.Candidates[1]
	rule.Include, rule.Enforcement, rule.DoNotApplyIf = true, "required", []string{"data:internal"}
	ruleFiles, err := FilesOf(rule)
	if err != nil || !strings.Contains(ruleFiles["rules/docs.md"], "enforcement: required\n") || !strings.Contains(ruleFiles["rules/docs.md"], "doNotApplyIf:\n    - data:internal\n") || !strings.Contains(ruleFiles["rules/docs.md"], "Cite sources.") {
		t.Fatalf("rule files %v err %v", ruleFiles, err)
	}
	broken := sample()
	broken.Candidates[0].Level = "organizations.acme.rules.x"
	broken.Candidates[0].ApplyIf = []string{"nope", "stack:aws", "language:rust", "data:public", "data:internal"}
	broken.Candidates[0].Enforcement = "maybe"
	broken.Candidates[0].Files = nil
	broken.Candidates = append(broken.Candidates, broken.Candidates[0])
	broken.MarkProposed("rein/x", "https://github.com/acme/pr/1", time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC))
	joined := strings.Join(broken.Check(registry), "\n")
	for _, expected := range []string{"proposed on 2026-10-23 as https://github.com/acme/pr/1", `level "organizations.acme.rules.x" is not one`, "appears twice", `label "nope" is not category:value`, `label "stack:aws" names no category`, `label "language:rust" is not in the registry; language has go`, "applyIf data takes one value; given public, internal", `enforcement "maybe"`, "skills/go-style: no files"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("missing %q in %s", expected, joined)
		}
	}
}
