package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Danidani23/agents-rein/internal/declaration"
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
