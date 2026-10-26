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
{"version": 2, "candidates": [
  {"candidate": "rules/go-testing", "verdict": "overlaps", "related": ["organizations.acme.teams.platform.rules.go-style"], "quotes": [{"address": "organizations.acme.teams.platform.rules.go-style", "text": "table-driven   tests only"}], "suggestion": "drop", "reason": "the team rule already says it"},
  {"candidate": "rules/brief", "verdict": "new", "suggestion": "lift", "level": "organization", "applyIf": [], "everywhere": "brevity helps every kind of project", "reason": "every project benefits"}
]}
` + "```"

func TestBrief(t *testing.T) {
	brief := Brief("organizations.acme.projects.shop", exampleCandidates(), exampleCorpus())
	for _, expected := range []string{"## rules/go-testing", "Use table-driven tests only.", "## organizations.acme.teams.platform.rules.go-style (applies, required, applyIf language:go)", "## organizations.acme.teams.data.rules.notebooks (other team)", `"version": 2`, "Answer with JSON only"} {
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
	candidates = Apply(answer, candidates)
	if candidates[0].Judgement == nil || candidates[0].Judgement.Suggestion != "drop" || candidates[1].Judgement.Level != "organization" {
		t.Fatalf("judgements %+v %+v", candidates[0].Judgement, candidates[1].Judgement)
	}
	_, err = ParseAnswer("no json here")
	if err == nil {
		t.Fatalf("prose parsed")
	}
	bad := Answer{Version: 3, Candidates: []AnswerCandidate{
		{Candidate: "rules/go-testing", Judgement: Judgement{Verdict: "maybe", Related: []string{"organizations.acme.rules.nope"}, Quotes: []Quote{{Address: "organizations.acme.teams.platform.rules.go-style", Text: "never written there"}}, Suggestion: "lift", Reason: ""}},
		{Candidate: "rules/unknown", Judgement: Judgement{Verdict: "new", Suggestion: "keep", Reason: "x"}},
		{Candidate: "rules/brief", Judgement: Judgement{Verdict: "new", Suggestion: "merge", Reason: "x"}},
	}}
	problems = Validate(bad, candidates, exampleCorpus())
	joined := strings.Join(problems, "\n")
	for _, expected := range []string{"version 3", `verdict "maybe"`, `related "organizations.acme.rules.nope"`, "is not verbatim", "lift needs a level", "no reason", `"rules/unknown" is not a candidate`, `suggestion "merge" is not one of keep, drop, lift`} {
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

// mixedCandidates is a section that repeats a team rule beside a project
// fact, and a skill that hides this repository's definition of done.
func mixedCandidates() []Candidate {
	return []Candidate{
		{Kind: "rules", ID: "git", Sources: []string{"CLAUDE.md"}, Files: map[string]string{"rules/git.md": "## Git\n\nTests are table-driven tests only.\n\nmain is also the production branch.\n"}},
		{Kind: "skills", ID: "plan", Sources: []string{".claude/skills/plan/SKILL.md"}, Files: map[string]string{"skills/plan/SKILL.md": "---\nname: plan\ndescription: Plans.\n---\n\nWrite the board.\n\nIn this repo, an item is done when task verify passes.\n", "skills/plan/notes.md": "board notes\n"}},
		{Kind: "permissions", ID: "settings", Files: map[string]string{"permissions/settings.yaml": "format: claude-code\nallow: [\"Bash(go test*)\"]\n"}},
	}
}

const unitsAnswer = `{"version": 2,
"candidates": [{"candidate": "permissions/settings", "verdict": "new", "suggestion": "keep", "reason": "fine"}],
"units": [
  {"id": "git-repeat", "kind": "rules", "from": [{"candidate": "rules/git", "lines": "1, 3"}], "shape": "always", "verdict": "duplicate", "related": ["organizations.acme.teams.platform.rules.go-style"], "quotes": [{"address": "organizations.acme.teams.platform.rules.go-style", "text": "table-driven tests only"}], "suggestion": "drop", "reason": "the team says it"},
  {"id": "production-branch", "kind": "rules", "from": [{"candidate": "rules/git", "lines": "5"}], "shape": "paths", "paths": ["deploy/**"], "heading": "## Branches", "verdict": "new", "suggestion": "keep", "reason": "a project fact"},
  {"id": "plan", "kind": "skills", "from": [{"candidate": "skills/plan", "file": "SKILL.md", "lines": "2"}, {"candidate": "skills/plan", "file": "notes.md"}], "shape": "skill", "description": "Write the board of a plan.", "verdict": "new", "suggestion": "keep", "reason": "a procedure"},
  {"id": "done-here", "kind": "rules", "from": [{"candidate": "skills/plan", "lines": "4"}], "shape": "always", "verdict": "new", "suggestion": "keep", "reason": "this repository's definition of done"}
]}`

func TestUnits(t *testing.T) {
	brief := Brief("organizations.acme.projects.shop", mixedCandidates(), exampleCorpus())
	// the distiller sees numbered lines, a skill's frontmatter apart, and how to cut
	for _, expected := range []string{"   5 | main is also the production branch.", "   4 | In this repo, an item is done when task verify passes.", "--- notes.md ---\nboard notes", "units", "verbatim"} {
		if !strings.Contains(brief, expected) {
			t.Fatalf("brief lacks %q:\n%s", expected, brief)
		}
	}
	answer, err := ParseAnswer(unitsAnswer)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	candidates := mixedCandidates()
	problems := Validate(answer, candidates, exampleCorpus())
	if len(problems) != 0 {
		t.Fatalf("problems %v", problems)
	}
	indexed := byID(Apply(answer, candidates))
	if len(indexed) != 5 || indexed["rules/git"].ID != "" || indexed["skills/plan"].Files["skills/plan/notes.md"] != "board notes\n" || indexed["permissions/settings"].Judgement == nil {
		t.Fatalf("applied %v", indexed)
	}
	repeat := indexed["rules/git-repeat"]
	if repeat.Files["rules/git-repeat.md"] != "## Git\n\nTests are table-driven tests only.\n" || repeat.Judgement.Suggestion != "drop" || strings.Join(repeat.Sources, ",") != "CLAUDE.md" {
		t.Fatalf("repeat %+v", repeat)
	}
	branch := indexed["rules/production-branch"].Files["rules/production-branch.md"]
	if !strings.HasPrefix(branch, "---\npaths:\n") || !strings.Contains(branch, "deploy/**") || !strings.HasSuffix(branch, "---\n## Branches\n\nmain is also the production branch.\n") {
		t.Fatalf("branch %q", branch)
	}
	if indexed["skills/plan"].Files["skills/plan/SKILL.md"] != "---\nname: plan\ndescription: Write the board of a plan.\n---\n\nWrite the board.\n" || indexed["rules/done-here"].Files["rules/done-here.md"] != "In this repo, an item is done when task verify passes.\n" {
		t.Fatalf("skill %+v rule %+v", indexed["skills/plan"].Files, indexed["rules/done-here"].Files)
	}
	if !strings.Contains(strings.Join(indexed["rules/production-branch"].Notes, " "), "line 5 of rules/git") {
		t.Fatalf("notes %v", indexed["rules/production-branch"].Notes)
	}
	// every line of a cut candidate lands in exactly one unit, taken as it is
	bad := `{"version": 2,
"candidates": [{"candidate": "rules/extra", "verdict": "new", "suggestion": "keep", "reason": "x"}],
"units": [
  {"id": "e", "kind": "rules", "from": [{"candidate": "rules/extra", "lines": "1"}], "shape": "always", "verdict": "new", "suggestion": "keep", "reason": "x"},
  {"id": "a", "kind": "rules", "from": [{"candidate": "rules/git", "lines": "1, 3, 9"}], "shape": "always", "verdict": "new", "suggestion": "keep", "reason": "x"},
  {"id": "a", "kind": "rules", "from": [{"candidate": "rules/git", "lines": "3-5"}], "shape": "skill", "verdict": "new", "suggestion": "keep", "reason": "x"},
  {"id": "b", "kind": "skills", "from": [{"candidate": "skills/plan", "lines": "2"}], "shape": "skill", "verdict": "new", "suggestion": "keep", "reason": "x"},
  {"id": "c", "kind": "rules", "from": [{"candidate": "permissions/settings", "lines": "1"}, {"candidate": "skills/plan", "lines": "x"}], "shape": "paths", "verdict": "new", "suggestion": "keep", "reason": "x"}
]}`
	answer, err = ParseAnswer(bad)
	if err != nil {
		t.Fatalf("parse bad: %v", err)
	}
	withExtra := append(mixedCandidates(), Candidate{Kind: "rules", ID: "extra", Files: map[string]string{"rules/extra.md": "Extra.\n"}})
	joined := strings.Join(Validate(answer, withExtra, exampleCorpus()), "\n")
	for _, expected := range []string{
		"rules/extra is answered whole and cut into units",
		"unit a: line 9 of rules/git is past its 5 lines",
		"rules/git: line 3 is in two units",
		"unit a: the id is taken",
		`unit a: shape "skill" does not fit rules`,
		"unit b: a skill needs a description",
		"unit c: permissions/settings is not a rule or a skill",
		`unit c: lines "x" of skills/plan are not numbers and ranges`,
		"unit c: shape paths needs paths",
		"skills/plan: line 4 is in no unit",
		"skills/plan: notes.md is in no unit",
		"permissions/settings is not answered",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("missing %q in\n%s", expected, joined)
		}
	}
}

func TestDocuments(t *testing.T) {
	architecture := []Candidate{{Kind: "rules", ID: "architecture", Sources: []string{"CLAUDE.md"}, Files: map[string]string{"rules/architecture.md": "## Architecture\n\nsrc/ holds the pages; functions/ the API.\n\nNever hardcode a colour.\n"}}}
	corpus := exampleCorpus()
	corpus.Documents = []string{"README.md", "docs/decisions/2026-09-01_progress.md"}
	brief := Brief("organizations.acme.projects.shop", architecture, corpus)
	for _, expected := range []string{"`document`", "- docs/decisions/2026-09-01_progress.md"} {
		if !strings.Contains(brief, expected) {
			t.Fatalf("brief lacks %q:\n%s", expected, brief)
		}
	}
	// without the project's repository there is nowhere to put a document
	if strings.Contains(Brief("organizations.acme.users.dani", architecture, exampleCorpus()), "`document`") {
		t.Fatalf("a document offered without a repository")
	}
	answer, _ := ParseAnswer(`{"version": 2, "candidates": [], "units": [
	  {"id": "architecture", "kind": "documents", "from": [{"candidate": "rules/architecture", "lines": "1-3"}], "shape": "document", "path": "docs/architecture.md", "pointer": "Before moving code between folders, read docs/architecture.md.", "paths": ["src/**", "functions/**"], "verdict": "new", "suggestion": "keep", "reason": "knowledge about the code"},
	  {"id": "colours", "kind": "rules", "from": [{"candidate": "rules/architecture", "lines": "5"}], "shape": "always", "verdict": "new", "suggestion": "keep", "reason": "how the agent works"}
	]}`)
	candidates := architecture
	problems := Validate(answer, candidates, corpus)
	if len(problems) != 0 {
		t.Fatalf("problems %v", problems)
	}
	indexed := byID(Apply(answer, candidates))
	document := indexed["documents/architecture"]
	if document.Path != "docs/architecture.md" || document.Files["documents/architecture.md"] != "## Architecture\n\nsrc/ holds the pages; functions/ the API.\n" {
		t.Fatalf("document %+v", document)
	}
	// the pointer is a rule every agent reads, path-scoped here, naming its document
	pointer := indexed["rules/architecture"].Files["rules/architecture.md"]
	if !strings.HasPrefix(pointer, "---\n") || !strings.Contains(pointer, "document: docs/architecture.md\n") || !strings.Contains(pointer, "src/**") || !strings.HasSuffix(pointer, "---\nBefore moving code between folders, read docs/architecture.md.\n") || indexed["rules/architecture"].Judgement == nil {
		t.Fatalf("pointer %q", pointer)
	}
	// a document is a new file the pointer names, in a project with a repository
	bad, _ := ParseAnswer(`{"version": 2, "candidates": [], "units": [
	  {"id": "a", "kind": "documents", "from": [{"candidate": "rules/architecture", "lines": "1-3"}], "shape": "document", "path": "README.md", "pointer": "Read the readme.", "verdict": "new", "suggestion": "keep", "reason": "x"},
	  {"id": "b", "kind": "documents", "from": [{"candidate": "rules/architecture", "lines": "5"}], "shape": "document", "path": "../notes.txt", "pointer": "Read it.\nTwice.", "verdict": "new", "suggestion": "keep", "reason": "x"}
	]}`)
	joined := strings.Join(Validate(bad, architecture, corpus), "\n")
	for _, expected := range []string{
		"unit a: README.md exists in the repository; a document is a new file",
		"unit a: the pointer must name README.md",
		"unit b: path \"../notes.txt\" is not a Markdown file inside the repository",
		"unit b: the pointer is one line",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("missing %q in\n%s", expected, joined)
		}
	}
	joined = strings.Join(Validate(answer, architecture, exampleCorpus()), "\n")
	if !strings.Contains(joined, "unit architecture: a document needs the project's repository") {
		t.Fatalf("no repository: %s", joined)
	}
}

func TestLabels(t *testing.T) {
	corpus := exampleCorpus()
	corpus.Labels = []string{"kind:static-site", "kind:web-app", "language:go", "language:typescript"}
	corpus.ProjectLabels = []string{"kind:static-site"}
	corpus.Projects = []Project{{Slug: "shop", Teams: []string{"web"}, Labels: []string{"kind:static-site"}}, {Slug: "tool", Labels: []string{"kind:cli", "language:go"}}}
	brief := Brief("organizations.acme.projects.shop", exampleCandidates(), corpus)
	for _, expected := range []string{"`labels`", "`assign`", "`add`", "`redundant`", "The project carries kind:static-site.", "`multiple`", "`everywhere`", "# The organisation's projects", "- shop, team web: kind:static-site", "- tool: kind:cli, language:go"} {
		if !strings.Contains(brief, expected) {
			t.Fatalf("brief lacks %q:\n%s", expected, brief)
		}
	}
	answer, _ := ParseAnswer(`{"version": 2,
	"candidates": [
	  {"candidate": "rules/go-testing", "verdict": "new", "suggestion": "keep", "reason": "x"},
	  {"candidate": "rules/brief", "verdict": "new", "suggestion": "lift", "level": "organization", "applyIf": ["framework:astro"], "reason": "every Astro site"}],
	"labels": [
	  {"action": "assign", "label": "language:typescript", "reason": "the pages are TypeScript"},
	  {"action": "add", "label": "framework:astro", "meaning": "Sites built with Astro", "multiple": true, "reason": "the lifted brief needs it"},
	  {"action": "assign", "label": "framework:astro", "reason": "the shop is built with Astro"},
	  {"action": "redundant", "label": "kind:web-app", "reason": "the same as kind:static-site here"}]}`)
	candidates := exampleCandidates()
	problems := Validate(answer, candidates, corpus)
	if len(problems) != 0 {
		t.Fatalf("problems %v", problems)
	}
	indexed := byID(Apply(answer, candidates))
	add := indexed["labels/framework-astro"]
	if add.Files["labels/framework-astro.yaml"] != "action: add\nlabel: framework:astro\nmeaning: Sites built with Astro\nmultiple: true\n" || add.Judgement == nil || add.Judgement.Suggestion != "add" || add.Judgement.Reason != "the lifted brief needs it" {
		t.Fatalf("add %+v", add)
	}
	if indexed["labels/language-typescript"].Judgement.Suggestion != "assign" || indexed["labels/kind-web-app"].Judgement.Suggestion != "redundant" {
		t.Fatalf("labels %+v", indexed)
	}
	// a label change says what it changes and why, and a new label serves a lift
	bad, _ := ParseAnswer(`{"version": 2,
	"candidates": [
	  {"candidate": "rules/go-testing", "verdict": "new", "suggestion": "keep", "reason": "x"},
	  {"candidate": "rules/brief", "verdict": "new", "suggestion": "keep", "reason": "x"}],
	"labels": [
	  {"action": "assign", "label": "kind:static-site", "reason": "x"},
	  {"action": "assign", "label": "stack:rails", "reason": "x"},
	  {"action": "add", "label": "language:go", "reason": "x"},
	  {"action": "add", "label": "framework:astro", "reason": ""},
	  {"action": "retire", "label": "Kind Web", "reason": "x"}]}`)
	joined := strings.Join(Validate(bad, exampleCandidates(), corpus), "\n")
	for _, expected := range []string{
		"label assign kind:static-site: the project carries it",
		"label assign stack:rails: the registry has no such label; add it instead",
		"label add language:go: the registry has it; assign it instead",
		"label add framework:astro: a new label needs its meaning",
		"label add framework:astro: no entry or unit lifted in this answer uses it in applyIf",
		"label add framework:astro: no reason",
		`label retire Kind Web: the action is not assign, add or redundant`,
		`label retire Kind Web: "Kind Web" is not category:value`,
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("missing %q in\n%s", expected, joined)
		}
	}
	// a category of one value takes no second one, carried or assigned in the same answer
	corpus.SingleValued = []string{"kind"}
	if brief := Brief("organizations.acme.projects.shop", exampleCandidates(), corpus); !strings.Contains(brief, "These categories take one value per project: kind.") {
		t.Fatalf("brief lacks the categories of one value:\n%s", brief)
	}
	second, _ := ParseAnswer(`{"version": 2,
	"candidates": [
	  {"candidate": "rules/go-testing", "verdict": "new", "suggestion": "keep", "reason": "x"},
	  {"candidate": "rules/brief", "verdict": "new", "suggestion": "keep", "reason": "x"}],
	"labels": [
	  {"action": "assign", "label": "kind:web-app", "reason": "x"}]}`)
	joined = strings.Join(Validate(second, exampleCandidates(), corpus), "\n")
	if !strings.Contains(joined, "label assign kind:web-app: kind takes one value and the project carries kind:static-site") {
		t.Fatalf("second value: %s", joined)
	}
	corpus.ProjectLabels = []string{}
	two, _ := ParseAnswer(`{"version": 2,
	"candidates": [
	  {"candidate": "rules/go-testing", "verdict": "new", "suggestion": "keep", "reason": "x"},
	  {"candidate": "rules/brief", "verdict": "new", "suggestion": "keep", "reason": "x"}],
	"labels": [
	  {"action": "assign", "label": "kind:web-app", "reason": "x"},
	  {"action": "assign", "label": "kind:static-site", "reason": "x"},
	  {"action": "assign", "label": "language:go", "reason": "x"},
	  {"action": "assign", "label": "language:typescript", "reason": "x"}]}`)
	joined = strings.Join(Validate(two, exampleCandidates(), corpus), "\n")
	if !strings.Contains(joined, "label assign kind:static-site: kind takes one value and this answer assigns kind:web-app too") || strings.Contains(joined, "language") {
		t.Fatalf("two values: %s", joined)
	}
	// a new category says whether it takes several values; a lift says whom it is for and keeps its project (D40)
	corpus.ProjectLabels = []string{"kind:static-site"}
	lifts, _ := ParseAnswer(`{"version": 2,
	"candidates": [
	  {"candidate": "rules/go-testing", "verdict": "new", "suggestion": "lift", "level": "team", "applyIf": [], "reason": "x"},
	  {"candidate": "rules/brief", "verdict": "new", "suggestion": "lift", "level": "organization", "applyIf": ["framework:astro"], "reason": "x"}],
	"labels": [
	  {"action": "add", "label": "framework:astro", "meaning": "Sites built with Astro", "reason": "x"}]}`)
	joined = strings.Join(Validate(lifts, exampleCandidates(), corpus), "\n")
	for _, expected := range []string{
		"label add framework:astro: framework is a new category; say with multiple whether a project may carry several of its values",
		"rules/go-testing: lifted to the team without applyIf; name the labels of the projects it is for, or say in everywhere why it holds for every project",
		"rules/brief: applyIf framework:astro leaves out this project, which carries kind:static-site; assign one of them to it in labels too, or lift with a label it carries",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("missing %q in\n%s", expected, joined)
		}
	}
	// without a project there are no project labels to review
	if strings.Contains(Brief("organizations.acme.users.dani", exampleCandidates(), exampleCorpus()), "`assign`") {
		t.Fatalf("labels reviewed without a project")
	}
}

func TestAboutTheProject(t *testing.T) {
	corpus := exampleCorpus()
	corpus.ProjectReadme = "# Website\n\nTwo sites on Cloudflare Pages, infrastructure in Terraform.\n"
	corpus.ProjectFiles = []string{"astro.config.mjs", "package.json", "terraform/main.tf"}
	corpus.ProjectFilesTotal = 412
	brief := Brief("organizations.acme.projects.shop", exampleCandidates(), corpus)
	for _, expected := range []string{"# About the project", "Two sites on Cloudflare Pages, infrastructure in Terraform.", "names only, 3 of 412", "- astro.config.mjs\n- package.json\n- terraform/main.tf"} {
		if !strings.Contains(brief, expected) {
			t.Fatalf("brief lacks %q:\n%s", expected, brief)
		}
	}
	if strings.Contains(Brief("organizations.acme.users.dani", exampleCandidates(), exampleCorpus()), "# About the project") {
		t.Fatalf("a project described without one")
	}
	// a long README is cut at a line, within the budget
	long := strings.Repeat("A line of the README.\n", 1000)
	excerpt := ReadmeExcerpt(long)
	if len(excerpt) > readmeBudget || !strings.HasSuffix(excerpt, "A line of the README.\n") {
		t.Fatalf("excerpt of %d characters ends %q", len(excerpt), excerpt[len(excerpt)-30:])
	}
}
