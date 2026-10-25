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
	// proposal.yaml holds the proposal's own fields, each candidate is a file of its own
	index, _ := os.ReadFile(filepath.Join(folder, IndexFile))
	text := string(index)
	for _, expected := range []string{Header, "version: 3\n", "id: 3f9c2e1a7b4d\n", "status: pending\n", "pull_request: \"\"\n"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("missing %q in %s", expected, text)
		}
	}
	if strings.Contains(text, "candidates:") || strings.Contains(text, "proposed_at") {
		t.Fatalf("the index holds candidates or a time that has not happened: %s", text)
	}
	skill, _ := os.ReadFile(filepath.Join(folder, "skill_go-style.yaml"))
	rule, _ := os.ReadFile(filepath.Join(folder, "rule_docs.yaml"))
	for _, expected := range []string{CandidateHeader, "kind: skills\n", "id: go-style\n", "include: true\n", "level: organizations.acme\n", "applyIf:\n    - language:go\n", "files:\n", "SKILL.md: |", "references/errors.md: |"} {
		if !strings.Contains(string(skill), expected) {
			t.Fatalf("missing %q in %s", expected, skill)
		}
	}
	if !strings.Contains(string(rule), "include: false\n") || !strings.Contains(string(rule), "Cite sources.") {
		t.Fatalf("rule %s", rule)
	}
	entries, _ := os.ReadDir(folder)
	if len(entries) != 3 {
		t.Fatalf("the folder holds %d files, not proposal.yaml and two candidates", len(entries))
	}
	// candidates read in the order of their files' names
	read, err := Load(folder)
	if err != nil || read.ID != document.ID || len(read.Candidates) != 2 || read.Candidates[0].ID != "docs" || read.Candidates[1].Files["references/errors.md"] != "two lines\n" || read.Candidates[1].Judge.Level != "organizations.acme" || !read.Snapshot.TakenAt.Equal(document.Snapshot.TakenAt) {
		t.Fatalf("read %+v err %v", read, err)
	}
	// an edit in a candidate's file is what propose reads, and a deleted file leaves its candidate out
	_ = os.WriteFile(filepath.Join(folder, "rule_docs.yaml"), []byte(strings.Replace(string(rule), "Cite sources.", "Cite every source.", 1)), 0o600)
	read, _ = Load(folder)
	if !strings.Contains(read.Candidates[0].Files["docs.md"], "Cite every source.") {
		t.Fatalf("the edit was not read: %+v", read.Candidates[0].Files)
	}
	_ = os.Remove(filepath.Join(folder, "rule_docs.yaml"))
	read, _ = Load(folder)
	if len(read.Candidates) != 1 {
		t.Fatalf("a deleted candidate was read: %d", len(read.Candidates))
	}
	_ = os.WriteFile(filepath.Join(folder, "rule_broken.yaml"), []byte("notes: [x]\n"), 0o600)
	_, err = Load(folder)
	if err == nil || !strings.Contains(err.Error(), "rule_broken.yaml is not a candidate") {
		t.Fatalf("a file without kind and id: %v", err)
	}
	// a folder of before, proposal.yaml beside the entries it names, still reads
	entriesFolder := filepath.Join(t.TempDir(), "proposal-old")
	_ = os.MkdirAll(filepath.Join(entriesFolder, "skill_go-style", "references"), 0o700)
	_ = os.WriteFile(filepath.Join(entriesFolder, IndexFile), []byte("version: 2\nid: old\nstatus: pending\norganization: acme\ncandidates:\n  - kind: skills\n    id: go-style\n    include: true\n    entry: skill_go-style\n  - kind: rules\n    id: docs\n    entry: rule_docs.md\n"), 0o600)
	_ = os.WriteFile(filepath.Join(entriesFolder, "skill_go-style", "SKILL.md"), []byte("---\nname: go-style\n---\n"), 0o600)
	_ = os.WriteFile(filepath.Join(entriesFolder, "skill_go-style", "references", "errors.md"), []byte("two lines\n"), 0o600)
	_ = os.WriteFile(filepath.Join(entriesFolder, "rule_docs.md"), []byte("Cite sources.\n"), 0o600)
	read, err = Load(entriesFolder)
	if err != nil || len(read.Candidates) != 2 || read.Candidates[0].Files["references/errors.md"] != "two lines\n" || read.Candidates[1].Files["docs.md"] != "Cite sources.\n" {
		t.Fatalf("entries folder %+v err %v", read.Candidates, err)
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
	_ = os.WriteFile(filepath.Join(folder, IndexFile), []byte("version: 4\n"), 0o600)
	_, err = Load(folder)
	if err == nil || !strings.Contains(err.Error(), "version 4") {
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

func TestConflicts(t *testing.T) {
	conflicting := func(resolution string) Candidate {
		return Candidate{Kind: "rules", ID: "handover", Level: "organizations.acme.projects.shop", ApplyIf: []string{}, DoNotApplyIf: []string{}, Files: map[string]string{"handover.md": "## Handover\n\nOne block for all commands.\n"}, Conflict: &Conflict{With: []string{"organizations.acme.users.dani.rules.global"}, Resolution: resolution}}
	}
	document := Proposal{Version: Version, Status: StatusPending, Organization: "acme", Candidates: []Candidate{conflicting("")}}
	problems := strings.Join(document.Check(nil), "\n")
	if !strings.Contains(problems, "rules/handover conflicts with organizations.acme.users.dani.rules.global and is not resolved: set its resolution in proposal.yaml to drop, override or amend") {
		t.Fatalf("unresolved: %s", problems)
	}
	document.Candidates[0] = conflicting("maybe")
	problems = strings.Join(document.Check(nil), "\n")
	if !strings.Contains(problems, `rules/handover: resolution "maybe" is not drop, override or amend`) {
		t.Fatalf("unknown resolution: %s", problems)
	}
	// override puts it in, opening with the line that names what it replaces
	document.Candidates[0] = conflicting("override")
	levels, byLevel := document.Included()
	if len(document.Check(nil)) != 0 || len(levels) != 1 {
		t.Fatalf("override: problems %v levels %v", document.Check(nil), levels)
	}
	files, _ := FilesOf(byLevel[levels[0]][0])
	if files["rules/handover.md"] != "This replaces `organizations.acme.users.dani.rules.global` where the two disagree.\n\n## Handover\n\nOne block for all commands.\n" {
		t.Fatalf("override files %q", files)
	}
	// drop and amend leave it out, whatever include says
	for _, resolution := range []string{"drop", "amend"} {
		candidate := conflicting(resolution)
		candidate.Include = true
		document.Candidates[0] = candidate
		levels, _ = document.Included()
		if len(levels) != 0 || len(document.Excluded()) != 1 {
			t.Fatalf("%s: levels %v excluded %d", resolution, levels, len(document.Excluded()))
		}
	}
}

func TestDocumentEntry(t *testing.T) {
	document := distill.Candidate{Kind: distill.KindDocuments, ID: "architecture", Path: "docs/architecture.md", Files: map[string]string{"documents/architecture.md": "## Architecture\n\nsrc/ holds the pages.\n"}}
	proposalDocument := Proposal{Version: Version, ID: "abc", Status: StatusPending, Organization: "acme", DistilledFrom: "organizations.acme.projects.shop", Candidates: []Candidate{FromCandidate(document, "organizations.acme.projects.shop", true, nil, nil)}}
	folder := filepath.Join(t.TempDir(), FolderName("abc"))
	err := proposalDocument.WriteFolder(folder)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	written, _ := os.ReadFile(filepath.Join(folder, "document_architecture.yaml"))
	if !strings.Contains(string(written), "path: docs/architecture.md\n") || !strings.Contains(string(written), "architecture.md: |") {
		t.Fatalf("document %s", written)
	}
	read, err := Load(folder)
	if err != nil || read.Candidates[0].Path != "docs/architecture.md" || read.Candidates[0].Files["architecture.md"] != "## Architecture\n\nsrc/ holds the pages.\n" {
		t.Fatalf("read %+v err %v", read.Candidates, err)
	}
	read.Candidates[0].Path = "../outside.md"
	if !strings.Contains(strings.Join(read.Check(nil), "\n"), `documents/architecture: path "../outside.md" is not a Markdown file inside the repository`) {
		t.Fatalf("check %v", read.Check(nil))
	}
}
