package distill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Danidani23/agents-rein/internal/app/2_services/lint"
)

// harness is a Claude Code project as a snapshot holds it.
var harness = map[string]string{
	"CLAUDE.md":                      "# Shop\n\nBe brief.\n",
	"CLAUDE.local.md":                "Mine.\n",
	".claude/rules/Go Style.md":      "---\npaths:\n  - \"**/*.go\"\n---\nTwo-line errors.\n",
	".claude/skills/commit/SKILL.md": "---\nname: commit\ndescription: Draft commits.\nargument-hint: \"[message]\"\n---\n",
	".claude/skills/commit/extra.md": "more\n",
	".claude/settings.json":          `{"permissions":{"allow":["Bash(go test*)"],"deny":["Bash(git push --force*)"]},"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo hi","timeout":10}]}]}}`,
	".mcp.json":                      `{"mcpServers":{"GitHub":{"command":"npx","args":["-y","@modelcontextprotocol/server-github"],"env":{"GITHUB_PERSONAL_ACCESS_TOKEN":"ghp_abcdefghijklmnopqrstuvwxyz0123456789","LOG":"debug"}},"docs":{"type":"http","url":"https://docs.example/mcp"}}}`,
	".claude/agents/reviewer.md":     "an agent",
	".claude/todo.txt":               "not a harness file",
}

func byID(candidates []Candidate) map[string]Candidate {
	indexed := map[string]Candidate{}
	for _, candidate := range candidates {
		indexed[candidate.Kind+"/"+candidate.ID] = candidate
	}
	return indexed
}

func TestConvertClaudeCode(t *testing.T) {
	candidates, skipped, err := ConvertClaudeCode(harness)
	if err != nil || len(candidates) != 8 || len(skipped) != 2 {
		t.Fatalf("candidates %d skipped %v err %v", len(candidates), skipped, err)
	}
	indexed := byID(candidates)
	for _, expected := range []string{"rules/instructions", "rules/local-instructions", "rules/go-style", "skills/commit", "permissions/settings", "hooks/pretooluse-1", "mcpservers/github", "mcpservers/docs"} {
		_, found := indexed[expected]
		if !found {
			t.Fatalf("missing %s in %v", expected, candidates)
		}
	}
	if len(indexed["skills/commit"].Files) != 2 || indexed["rules/go-style"].Files["rules/go-style.md"] != harness[".claude/rules/Go Style.md"] {
		t.Fatalf("skill %+v rule %+v", indexed["skills/commit"], indexed["rules/go-style"])
	}
	commit := indexed["skills/commit"]
	if commit.Files["skills/commit/SKILL.md"] != "---\nname: commit\ndescription: Draft commits.\n---\n" || len(commit.Notes) != 1 || commit.Notes[0] != `removed argument-hint: "[message]" from SKILL.md; the Agent Skills specification has no such field` {
		t.Fatalf("skill %q notes %q", commit.Files["skills/commit/SKILL.md"], commit.Notes)
	}
	hook := indexed["hooks/pretooluse-1"]
	if hook.Files["hooks/pretooluse-1/run.sh"] != "#!/bin/sh\necho hi\n" || !strings.Contains(hook.Files["hooks/pretooluse-1/hook.yaml"], "event: PreToolUse") || !strings.Contains(hook.Files["hooks/pretooluse-1/hook.yaml"], "timeout_seconds: 10") {
		t.Fatalf("hook %+v", hook)
	}
	github := indexed["mcpservers/github"]
	server := github.Files["mcpservers/github.yaml"]
	if strings.Contains(server, "ghp_") || !strings.Contains(server, "GITHUB_PERSONAL_ACCESS_TOKEN: ${REIN_GITHUB_GITHUB_PERSONAL_ACCESS_TOKEN}") || !strings.Contains(server, "LOG: debug") || !strings.Contains(server, "variables:") || len(github.Notes) != 2 {
		t.Fatalf("server %q notes %v", server, github.Notes)
	}
	if !strings.Contains(indexed["mcpservers/docs"].Files["mcpservers/docs.yaml"], "transport: http") || !strings.Contains(indexed["rules/local-instructions"].Notes[0], "user level") {
		t.Fatalf("docs %+v local %+v", indexed["mcpservers/docs"], indexed["rules/local-instructions"])
	}
}

func TestCutAtSections(t *testing.T) {
	instructions := "# Website\n\nTwo sites in one repo.\n\n## Privacy posture — the invariant that matters most\n\nNothing links to the site.\n\n```markdown\n## not a heading, inside a fence\n```\n\n## Git\n\nThe user commits.\n\n## Git\n\nAgain.\n"
	infra := "---\npaths:\n  - \"terraform/**\"\n---\nInfrastructure first.\n\n## State\n\nState is committed.\n"
	mixed := "---\nname: mixed\ndescription: Two things.\n---\n\n## One\n\nA.\n\n## Two\n\nB.\n"
	candidates, _, err := ConvertClaudeCode(map[string]string{"CLAUDE.md": instructions, ".claude/rules/infra.md": infra, ".claude/skills/mixed/SKILL.md": mixed})
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	indexed := byID(candidates)
	// the text before the first section keeps the file's id; each section is a unit of its own
	for key, content := range map[string]string{
		"rules/instructions":    "# Website\n\nTwo sites in one repo.\n",
		"rules/privacy-posture": "## Privacy posture — the invariant that matters most\n\nNothing links to the site.\n\n```markdown\n## not a heading, inside a fence\n```\n",
		"rules/git":             "## Git\n\nThe user commits.\n",
		"rules/git-2":           "## Git\n\nAgain.\n",
		// a path-scoped rule stays path-scoped in every part
		"rules/infra":       "---\npaths:\n  - \"terraform/**\"\n---\nInfrastructure first.\n",
		"rules/infra-state": "---\npaths:\n  - \"terraform/**\"\n---\n## State\n\nState is committed.\n",
	} {
		candidate, found := indexed[key]
		if !found || candidate.Files["rules/"+candidate.ID+".md"] != content {
			t.Fatalf("%s: found %v content %q", key, found, candidate.Files)
		}
	}
	if !strings.Contains(indexed["rules/privacy-posture"].Notes[0], `the section "Privacy posture — the invariant that matters most" of CLAUDE.md`) || strings.Join(indexed["rules/git-2"].Sources, ",") != "CLAUDE.md" {
		t.Fatalf("notes %v sources %v", indexed["rules/privacy-posture"].Notes, indexed["rules/git-2"].Sources)
	}
	// a skill stays whole; the distiller splits one that mixes concerns
	if len(indexed["skills/mixed"].Files) != 1 || len(candidates) != 7 {
		t.Fatalf("skill %+v candidates %d", indexed["skills/mixed"], len(candidates))
	}
}

// A line of prose that starts with backticks is no code block: a backtick
// fence's info string holds no backtick (CommonMark), so the sections after
// it are still cut. The website's instructions had such a line.
func TestCutIgnoresBackticksInProse(t *testing.T) {
	instructions := "## Git\n\nhand over ONE copy-paste-runnable\n```bash block with explicit `git add <paths>` per commit.\n\n## Architecture\n\n```\napex/  the apex site\n```\n\n## Privacy\n\nNothing links to the site.\n\n~~~text\n## inside a tilde fence\n~~~\n\n## Terminology\n\nCorrect wrong terms.\n"
	candidates, _, err := ConvertClaudeCode(map[string]string{"CLAUDE.md": instructions})
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	indexed := byID(candidates)
	for _, expected := range []string{"rules/git", "rules/architecture", "rules/privacy", "rules/terminology"} {
		if _, found := indexed[expected]; !found {
			t.Fatalf("missing %s in %v", expected, indexed)
		}
	}
	if !strings.Contains(indexed["rules/privacy"].Files["rules/privacy.md"], "## inside a tilde fence") || len(candidates) != 4 {
		t.Fatalf("privacy %q candidates %d", indexed["rules/privacy"].Files["rules/privacy.md"], len(candidates))
	}
}

func TestDistilledCandidatesPassTheLint(t *testing.T) {
	candidates, _, _ := ConvertClaudeCode(harness)
	root := t.TempDir()
	level := filepath.Join(root, "declarations", "projects", "shop")
	for _, candidate := range candidates {
		for relative, content := range candidate.Files {
			target := filepath.Join(level, filepath.FromSlash(relative))
			_ = os.MkdirAll(filepath.Dir(target), 0o755)
			_ = os.WriteFile(target, []byte(content), 0o644)
		}
	}
	report, err := lint.Run(root)
	if err != nil || report.Errors() != 0 {
		var messages []string
		for _, finding := range report.Findings {
			messages = append(messages, finding.Path+": "+finding.Message)
		}
		t.Fatalf("err %v findings %v", err, messages)
	}
}

func TestCatalogueAndCompare(t *testing.T) {
	root := t.TempDir()
	write := func(relative string, content string) {
		target := filepath.Join(root, "declarations", filepath.FromSlash(relative))
		_ = os.MkdirAll(filepath.Dir(target), 0o755)
		_ = os.WriteFile(target, []byte(content), 0o644)
	}
	write("organization.yaml", "id: 0f8fad5b-d9cb-469f-a165-70867728950e\nslug: acme\nname: Acme\nhost: github\n")
	write("rules/go-style.md", harness[".claude/rules/Go Style.md"])
	write("rules/instructions.md", "# Acme\n\nDifferent.\n")
	write("teams/web/mcpservers/github.yaml", "description: elsewhere\ntransport: stdio\ncommand: npx\nargs: [\"-y\", \"@modelcontextprotocol/server-github\"]\n")
	write("projects/shop/skills/commit/SKILL.md", "other content")
	write("users/danidani23/hooks/pretooluse-1/hook.yaml", "format: claude-code\nevent: PreToolUse\nmatcher: Bash\nrun: run.sh\n")
	write("users/danidani23/hooks/pretooluse-1/run.sh", "#!/bin/sh\necho hi\n")
	catalogue, err := LoadCatalogue(root)
	if err != nil || len(catalogue.Entries) != 5 || catalogue.Entries[0].Address != "organizations.acme.projects.shop.skills.commit" {
		t.Fatalf("catalogue %+v err %v", catalogue, err)
	}
	candidates, _, _ := ConvertClaudeCode(harness)
	Compare(candidates, catalogue, "organizations.acme.projects.shop")
	indexed := byID(candidates)
	cases := map[string][2]string{
		"rules/go-style":       {VerdictIdentical, "organizations.acme.rules.go-style"},
		"mcpservers/github":    {VerdictIdentical, "organizations.acme.teams.web.mcpservers.github"},
		"hooks/pretooluse-1":   {VerdictIdentical, "organizations.acme.users.danidani23.hooks.pretooluse-1"},
		"rules/instructions":   {VerdictSameID, "organizations.acme.rules.instructions"},
		"skills/commit":        {VerdictNew, ""},
		"permissions/settings": {VerdictNew, ""},
	}
	for id, expected := range cases {
		candidate := indexed[id]
		related := strings.Join(candidate.Related, ",")
		if candidate.Verdict != expected[0] || related != expected[1] {
			t.Fatalf("%s: verdict %s related %q, expected %v", id, candidate.Verdict, related, expected)
		}
	}
	_, err = LoadCatalogue(t.TempDir())
	if err != nil {
		t.Fatalf("an empty clone: %v", err)
	}
}
