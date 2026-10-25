package declaration

import (
	"strings"
	"testing"
	"time"
)

func TestRuleDeclarationSelectors(t *testing.T) {
	rule, selectors, problems := ParseRuleDeclaration("go.md", []byte("---\npaths: [\"**/*.go\"]\napplyIf: [language:go, language:rust]\ndoNotApplyIf: [kind:library]\nenforcement: required\n---\nTwo-line errors.\n"))
	if len(problems) != 0 || len(rule.Paths) != 1 || strings.Join(selectors.ApplyIf, ",") != "language:go,language:rust" || selectors.DoNotApplyIf[0] != "kind:library" || selectors.Enforcement != "required" {
		t.Fatalf("rule %+v selectors %+v problems %v", rule, selectors, problems)
	}
	// a rule may point at a document of the project's repository, which sync checks
	pointer, _, problems := ParseRuleDeclaration("overview.md", []byte("---\ndocument: docs/overview.md\n---\nRead docs/overview.md first.\n"))
	if len(problems) != 0 || pointer.Document != "docs/overview.md" {
		t.Fatalf("pointer %+v problems %v", pointer, problems)
	}
	_, selectors, problems = ParseRuleDeclaration("x.md", []byte("---\nenforcement: strict\napplyIf: [nocolon]\nextra: 1\n---\n"))
	joined := ""
	for _, problem := range problems {
		joined += string(problem.Severity) + ":" + problem.Message + "\n"
	}
	for _, expected := range []string{`enforcement "strict" is not`, `label "nocolon" is not category:value`, `warning:frontmatter field "extra"`} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("missing %q in %q", expected, joined)
		}
	}
	_, selectors, problems = ParseRuleDeclaration("plain.md", []byte("No frontmatter.\n"))
	if len(problems) != 0 || selectors.Enforcement != "" {
		t.Fatalf("plain rule: %+v %v", selectors, problems)
	}
}

func TestSkillSelectors(t *testing.T) {
	skill, _, _ := ParseSkill("SKILL.md", "commit", []byte("---\nname: commit\ndescription: Draft commits.\nmetadata:\n  rein-apply-if: \"language:go language:rust\"\n  rein-enforcement: required\n---\n"))
	selectors, problems := SkillSelectors(skill)
	if len(problems) != 0 || strings.Join(selectors.ApplyIf, ",") != "language:go,language:rust" || selectors.Enforcement != "required" {
		t.Fatalf("selectors %+v problems %v", selectors, problems)
	}
	skill, _, _ = ParseSkill("SKILL.md", "commit", []byte("---\nname: commit\ndescription: Draft commits.\nmetadata:\n  rein-enforcement: maybe\n---\n"))
	_, problems = SkillSelectors(skill)
	if len(problems) != 1 {
		t.Fatalf("problems %v", problems)
	}
}

func TestParseException(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	exception, problems := ParseException("legacy", []byte("declaration: organizations.acme.permissions.no-force-push\ngrantee: organizations.acme.teams.data\nreason: the data pipeline force-pushes its scratch branch\nuntil: 2027-01-01T00:00:00Z\n"), now)
	if len(problems) != 0 || exception.Grantee != "organizations.acme.teams.data" || exception.Expired(now) {
		t.Fatalf("exception %+v problems %v", exception, problems)
	}
	_, problems = ParseException("bad", []byte("declaration: organizations.acme\ngrantee: organizations.acme.rules.x\nuntil: 2026-01-01T00:00:00Z\n"), now)
	joined := ""
	for _, problem := range problems {
		joined += problem.Message + "\n"
	}
	for _, expected := range []string{"is not the address of a declaration", "is not the address of a team or a project", "reason is required", "expired on 2026-01-01"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("missing %q in %q", expected, joined)
		}
	}
	_, problems = ParseException("user", []byte("declaration: organizations.acme.rules.x\ngrantee: organizations.acme.users.dani\nreason: why\n"), now)
	if len(problems) != 1 || !strings.Contains(problems[0].Message, "not a team or a project") {
		t.Fatalf("problems %v", problems)
	}
}

func TestSkipEntries(t *testing.T) {
	_, problems := ParseTeam("web", []byte("id: 1b4e28ba-2fa1-11d2-883f-0016d3cca427\nslug: web\nname: Web\nskip: [organizations.acme.rules.style, organizations.acme, nonsense]\n"))
	if len(problems) != 2 {
		t.Fatalf("problems %v", problems)
	}
}
