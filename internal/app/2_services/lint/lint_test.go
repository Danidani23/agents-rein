package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
)

func writeFile(t *testing.T, root string, relative string, content string) {
	t.Helper()
	fullPath := filepath.Join(root, relative)
	err := os.MkdirAll(filepath.Dir(fullPath), 0o755)
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	err = os.WriteFile(fullPath, []byte(content), 0o644)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
}

func skillFile(name string, description string) string {
	return "---\nname: " + name + "\ndescription: " + description + "\n---\n# " + name + "\n"
}

func mustRun(t *testing.T, root string) Report {
	t.Helper()
	report, err := Run(root)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return report
}

func messages(report Report) []string {
	var all []string
	for _, finding := range report.Findings {
		all = append(all, string(finding.Severity)+": "+finding.Message)
	}
	return all
}

func TestRun(t *testing.T) {
	t.Run("a valid skill, an unconditional rule and consistent settings pass", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "skills/commit/SKILL.md", skillFile("commit", "Draft commits."))
		writeFile(t, root, "rules/git.md", "# Git\n")
		writeFile(t, root, "settings.json", `{"permissions":{"allow":["Bash(git *)"],"deny":["Bash(git push*)"]}}`)
		report := mustRun(t, root)
		if len(report.Findings) != 0 {
			t.Fatalf("unexpected findings: %v", messages(report))
		}
	})
	t.Run("a skill whose name differs from its folder is an error", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "skills/commit/SKILL.md", skillFile("commits", "Draft commits."))
		report := mustRun(t, root)
		if report.Errors() != 1 || !strings.Contains(report.Findings[0].Message, "does not match the directory") {
			t.Fatalf("findings: %v", messages(report))
		}
	})
	t.Run("a description over the limit is an error", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "skills/long/SKILL.md", skillFile("long", strings.Repeat("x", 1025)))
		report := mustRun(t, root)
		if report.Errors() != 1 || !strings.Contains(report.Findings[0].Message, "maximum is 1024") {
			t.Fatalf("findings: %v", messages(report))
		}
	})
	t.Run("a frontmatter field outside the specification is an error", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "skills/adr/SKILL.md", "---\nname: adr\ndescription: Write an ADR.\nargument-hint: x\n---\n")
		report := mustRun(t, root)
		if report.Errors() != 1 || !strings.Contains(report.Findings[0].Message, "argument-hint") {
			t.Fatalf("findings: %v", messages(report))
		}
	})
	t.Run("a skill without frontmatter yields exactly one error", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "skills/bare/SKILL.md", "# Bare\n")
		report := mustRun(t, root)
		if report.Errors() != 1 {
			t.Fatalf("findings: %v", messages(report))
		}
	})
	t.Run("two skills with one name are warned about on both", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "toolkit/skills/commit/SKILL.md", skillFile("commit", "Draft commits."))
		writeFile(t, root, ".claude/skills/commit/SKILL.md", skillFile("commit", "Draft commits, older copy."))
		report := mustRun(t, root)
		if report.Errors() != 0 || report.Warnings() != 2 {
			t.Fatalf("findings: %v", messages(report))
		}
		if !strings.Contains(report.Findings[0].Message, "also declared at") {
			t.Fatalf("findings: %v", messages(report))
		}
	})
	t.Run("a rule whose paths is not a list is an error", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "rules/go.md", "---\npaths: \"**/*.go\"\n---\n")
		report := mustRun(t, root)
		if report.Errors() != 1 {
			t.Fatalf("findings: %v", messages(report))
		}
	})
	t.Run("a Markdown file outside a rules folder is not a rule", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "docs/notes.md", "---\npaths: 3\n---\n")
		report := mustRun(t, root)
		if len(report.Findings) != 0 {
			t.Fatalf("findings: %v", messages(report))
		}
	})
	t.Run("an allow rule covered by a deny rule is an error", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, ".claude/settings.json", `{"permissions":{"allow":["Bash(git push origin*)"],"deny":["Bash(git push*)"]}}`)
		report := mustRun(t, root)
		if report.Errors() != 1 || !strings.Contains(report.Findings[0].Message, "is dead") {
			t.Fatalf("findings: %v", messages(report))
		}
	})
	t.Run("invalid settings JSON is an error", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, ".claude/settings.local.json", `{"permissions":`)
		report := mustRun(t, root)
		if report.Errors() != 1 {
			t.Fatalf("findings: %v", messages(report))
		}
	})
	t.Run("inside a declarations tree a finding carries the address", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "declarations/organizations/dani/teams/web/skills/commit/SKILL.md", skillFile("commits", "Draft commits."))
		writeFile(t, root, "declarations/organizations/dani/rules/go-style.md", "---\npaths: 3\n---\n")
		report := mustRun(t, root)
		if report.Errors() != 2 {
			t.Fatalf("findings: %v", messages(report))
		}
		addresses := map[string]bool{}
		for _, finding := range report.Findings {
			addresses[finding.Address] = true
		}
		if !addresses["organizations.dani.teams.web.skills.commit"] || !addresses["organizations.dani.rules.go-style"] {
			t.Fatalf("addresses: %v", addresses)
		}
	})
	t.Run("a path inside a declarations tree that is not pairs is an error", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "declarations/organizations/dani/skills/SKILL.md", skillFile("skills", "Misplaced."))
		report := mustRun(t, root)
		found := false
		for _, finding := range report.Findings {
			if strings.Contains(finding.Message, "does not form an address") && finding.Severity == declaration.SeverityError {
				found = true
			}
		}
		if !found {
			t.Fatalf("findings: %v", messages(report))
		}
	})
	t.Run("a declarations folder that does not hold organizations or users is plain", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "declarations/notes/skills/commit/SKILL.md", skillFile("commit", "Draft commits."))
		report := mustRun(t, root)
		if len(report.Findings) != 0 {
			t.Fatalf("findings: %v", messages(report))
		}
	})
	t.Run("a missing root is an error of the run", func(t *testing.T) {
		_, err := Run(filepath.Join(t.TempDir(), "missing"))
		if err == nil {
			t.Fatalf("expected an error")
		}
	})
	t.Run("findings name the path relative to the root given", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "skills/bare/SKILL.md", "# Bare\n")
		report := mustRun(t, root)
		if report.Findings[0].Path != filepath.Join(root, "skills", "bare", "SKILL.md") {
			t.Fatalf("path %q", report.Findings[0].Path)
		}
	})
}

func TestRecords(t *testing.T) {
	organization := "id: 0f8fad5b-d9cb-469f-a165-70867728950e\nslug: acme\nname: Acme\nhost: github\n"
	team := "id: 1b4e28ba-2fa1-11d2-883f-0016d3cca427\nslug: web\nname: Web\n"
	t.Run("a consistent org repository lints clean", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "declarations/organizations/acme/organization.yaml", organization)
		writeFile(t, root, "declarations/organizations/acme/labels/language.yaml", "category: language\nlabels:\n  go: Go\n")
		writeFile(t, root, "declarations/organizations/acme/teams/web/team.yaml", team)
		writeFile(t, root, "declarations/organizations/acme/projects/shop/project.yaml", "id: 6ba7b810-9dad-11d1-80b4-00c04fd430c8\nslug: shop\nname: Shop\nteams: [web]\nlabels: [language:go]\n")
		writeFile(t, root, "declarations/users/danidani23/user.yaml", "slug: danidani23\ngithub: Danidani23\n")
		report := mustRun(t, root)
		if len(report.Findings) != 0 {
			t.Fatalf("findings: %v", messages(report))
		}
	})
	t.Run("a broken record and a bad reference are reported with their addresses", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "declarations/organizations/acme/organization.yaml", organization)
		writeFile(t, root, "declarations/organizations/acme/teams/web/team.yaml", "id: nope\nslug: web\nname: Web\n")
		writeFile(t, root, "declarations/organizations/acme/projects/shop/project.yaml", "id: 6ba7b810-9dad-11d1-80b4-00c04fd430c8\nslug: shop\nname: Shop\nteams: [web]\n")
		report := mustRun(t, root)
		if report.Errors() != 2 {
			t.Fatalf("findings: %v", messages(report))
		}
		addresses := map[string]int{}
		for _, finding := range report.Findings {
			addresses[finding.Address]++
		}
		if addresses["organizations.acme.teams.web"] != 1 || addresses["organizations.acme.projects.shop"] != 1 {
			t.Fatalf("addresses: %v findings %v", addresses, messages(report))
		}
	})
	t.Run("a label file outside a declarations tree is checked but has no address", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "labels/kind.yaml", "category: other\nlabels:\n  x: y\n")
		report := mustRun(t, root)
		if report.Errors() != 1 || report.Findings[0].Address != "" {
			t.Fatalf("findings: %v", messages(report))
		}
	})
}

func TestDeclarationFormats(t *testing.T) {
	organization := "id: 0f8fad5b-d9cb-469f-a165-70867728950e\nslug: acme\nname: Acme\nhost: github\n"
	server := "description: GitHub through the official server\napplyIf: [language:go]\nenforcement: default\ntransport: stdio\ncommand: npx\nargs: [\"-y\", \"@modelcontextprotocol/server-github\"]\nenv:\n  GITHUB_PERSONAL_ACCESS_TOKEN: ${REIN_GITHUB_TOKEN}\nvariables:\n  REIN_GITHUB_TOKEN: a fine-grained token with contents:read\n"
	permissions := "description: Never rewrite history\nenforcement: required\nformat: claude-code\ndeny:\n  - \"Bash(git push --force*)\"\nallow:\n  - \"Bash(go test*)\"\n"
	hook := "description: Format Go before a commit\napplyIf: [language:go]\nformat: claude-code\nevent: PreToolUse\nmatcher: \"Bash(git commit*)\"\nrun: run.sh\ntimeout_seconds: 30\n"
	t.Run("well-formed declarations lint clean, each with its address", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "declarations/organizations/acme/organization.yaml", organization)
		writeFile(t, root, "declarations/organizations/acme/mcpservers/github.yaml", server)
		writeFile(t, root, "declarations/organizations/acme/permissions/no-history-rewrites.yaml", permissions)
		writeFile(t, root, "declarations/organizations/acme/teams/web/hooks/gofmt/hook.yaml", hook)
		writeFile(t, root, "declarations/organizations/acme/teams/web/hooks/gofmt/run.sh", "#!/bin/sh\ngofmt -l .\n")
		report := mustRun(t, root)
		if len(report.Findings) != 0 {
			t.Fatalf("findings: %v", messages(report))
		}
		writeFile(t, root, "declarations/organizations/acme/mcpservers/github.yaml", server+"url: http://x\n")
		report = mustRun(t, root)
		if report.Errors() != 1 || report.Findings[0].Address != "organizations.acme.mcpservers.github" {
			t.Fatalf("findings: %+v", report.Findings)
		}
	})
	t.Run("a literal secret, an unprefixed or undescribed variable, and an unused description", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "mcpservers/github.yaml", "transport: stdio\ncommand: npx\nenv:\n  GITHUB_TOKEN: ghp_abcdefghijklmnopqrstuvwxyz0123456789\n  OTHER: ${GITHUB_TOKEN}\n  HOME_TOKEN: ${REIN_HOME_TOKEN}\nvariables:\n  REIN_UNUSED: described but unused\n")
		report := mustRun(t, root)
		expected := []string{"looks like a secret", "start with REIN_", "which variables does not describe", "nothing references it"}
		if report.Errors() != 4 || report.Warnings() != 1 {
			t.Fatalf("findings: %v", messages(report))
		}
		for _, fragment := range expected {
			if !strings.Contains(strings.Join(messages(report), "\n"), fragment) {
				t.Fatalf("missing %q in %v", fragment, messages(report))
			}
		}
	})
	t.Run("a permission set with an unknown format, no rules, a bad rule or a dead allow", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "permissions/a.yaml", "format: cursor\n")
		writeFile(t, root, "permissions/b.yaml", "format: claude-code\ndeny: [\"Bash(git push*)\"]\nallow: [\"Bash(git push --tags)\", \"not a rule!\"]\n")
		report := mustRun(t, root)
		joined := strings.Join(messages(report), "\n")
		for _, fragment := range []string{"format \"cursor\" is unknown", "at least one rule", "is not Tool or Tool(specifier)", "is dead"} {
			if !strings.Contains(joined, fragment) {
				t.Fatalf("missing %q in %v", fragment, messages(report))
			}
		}
	})
	t.Run("a hook with an unknown event, a missing script or a path as run", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "hooks/one/hook.yaml", "format: claude-code\nevent: OnSave\nrun: missing.sh\n")
		writeFile(t, root, "hooks/two/hook.yaml", "format: claude-code\nevent: Stop\nrun: ../elsewhere.sh\n")
		report := mustRun(t, root)
		joined := strings.Join(messages(report), "\n")
		for _, fragment := range []string{"event \"OnSave\" is not one Claude Code fires", "not in the hook's folder", "not a path"} {
			if !strings.Contains(joined, fragment) {
				t.Fatalf("missing %q in %v", fragment, messages(report))
			}
		}
	})
}

func TestResolutionChecks(t *testing.T) {
	organization := "id: 0f8fad5b-d9cb-469f-a165-70867728950e\nslug: acme\nname: Acme\nhost: github\n"
	root := t.TempDir()
	writeFile(t, root, "declarations/organizations/acme/organization.yaml", organization)
	writeFile(t, root, "declarations/organizations/acme/teams/web/team.yaml", "id: 1b4e28ba-2fa1-11d2-883f-0016d3cca427\nslug: web\nname: Web\nskip: [organizations.acme.rules.missing]\n")
	writeFile(t, root, "declarations/organizations/acme/projects/shop/project.yaml", "id: 6ba7b810-9dad-11d1-80b4-00c04fd430c8\nslug: shop\nname: Shop\nteams: [web]\n")
	writeFile(t, root, "declarations/organizations/acme/exceptions/old.yaml", "declaration: organizations.acme.rules.style\ngrantee: organizations.acme.teams.web\nreason: old\nuntil: 2020-01-01T00:00:00Z\n")
	writeFile(t, root, "declarations/organizations/acme/exceptions/nobody.yaml", "declaration: organizations.acme.rules.style\ngrantee: organizations.acme.teams.nobody\nreason: whoever\n")
	writeFile(t, root, "declarations/organizations/acme/rules/style.md", "---\nenforcement: required\n---\nStyle.\n")
	writeFile(t, root, "declarations/organizations/acme/teams/web/rules/style.md", "Our style.\n")
	writeFile(t, root, "declarations/organizations/acme/skills/commit/SKILL.md", "---\nname: commit\ndescription: Draft commits.\nmetadata:\n  rein-enforcement: sometimes\n---\n")
	report := mustRun(t, root)
	joined := strings.Join(messages(report), "\n")
	for _, expected := range []string{"skip names organizations.acme.rules.missing, which does not exist", "expired on 2020-01-01", "grantee organizations.acme.teams.nobody is not a team or a project", "for project shop: redeclares organizations.acme.rules.style, which is required", `enforcement "sometimes" is not`} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("missing %q in %v", expected, messages(report))
		}
	}
	for _, finding := range report.Findings {
		if strings.Contains(finding.Message, "redeclares") && finding.Address != "organizations.acme.teams.web.rules.style" {
			t.Fatalf("the resolution problem carries %q", finding.Address)
		}
	}
}
