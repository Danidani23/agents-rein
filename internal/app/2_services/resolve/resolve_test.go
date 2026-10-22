package resolve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
)

// orgClone writes an org repository with every situation the resolution
// order covers.
func orgClone(t *testing.T, projectDeclaresReact bool, expired bool) string {
	t.Helper()
	root := t.TempDir()
	write := func(relative string, content string) {
		target := filepath.Join(root, "declarations", filepath.FromSlash(relative))
		_ = os.MkdirAll(filepath.Dir(target), 0o755)
		_ = os.WriteFile(target, []byte(content), 0o644)
	}
	until := "2030-01-01T00:00:00Z"
	if expired {
		until = "2020-01-01T00:00:00Z"
	}
	write("organization.yaml", "id: 0f8fad5b-d9cb-469f-a165-70867728950e\nslug: acme\nname: Acme\nhost: github\n")
	write("labels/language.yaml", "category: language\nlabels:\n  go: Go\n  python: Python\n")
	write("labels/kind.yaml", "category: kind\nlabels:\n  service: a service\n  library: a library\n")
	write("teams/web/team.yaml", "id: 1b4e28ba-2fa1-11d2-883f-0016d3cca427\nslug: web\nname: Web\nskip: [organizations.acme.rules.nodocs]\n")
	write("teams/data/team.yaml", "id: 6ba7b810-9dad-11d1-80b4-00c04fd430c8\nslug: data\nname: Data\nskip: [organizations.acme.rules.style]\n")
	write("projects/shop/project.yaml", "id: 7ba7b810-9dad-11d1-80b4-00c04fd430c8\nslug: shop\nname: Shop\nteams: [web, data]\nlabels: [language:go, kind:service]\n")
	write("exceptions/data-style.yaml", "declaration: organizations.acme.rules.style\ngrantee: organizations.acme.teams.data\nreason: legacy\nuntil: "+until+"\n")
	write("rules/style.md", "---\nenforcement: required\n---\nHouse style.\n")
	write("rules/testing.md", "Tests at the organisation level.\n")
	write("rules/go.md", "---\napplyIf: [language:go]\n---\nGo rules.\n")
	write("rules/py.md", "---\napplyIf: [language:python]\n---\nPython rules.\n")
	write("rules/nodocs.md", "---\ndoNotApplyIf: [kind:library]\n---\nNo docs folder.\n")
	write("rules/security.md", "---\nenforcement: required\n---\nSecurity.\n")
	write("mcpservers/github.yaml", "enforcement: required\ntransport: stdio\ncommand: npx\n")
	write("skills/commit/SKILL.md", "---\nname: commit\ndescription: Draft commits.\nmetadata:\n  rein-apply-if: \"kind:service\"\n---\n")
	write("teams/web/rules/testing.md", "Tests the web way.\n")
	write("teams/web/rules/react.md", "React from web.\n")
	write("teams/web/mcpservers/github.yaml", "transport: stdio\ncommand: other\n")
	write("teams/data/rules/react.md", "React from data.\n")
	write("teams/ops/team.yaml", "id: 8ba7b810-9dad-11d1-80b4-00c04fd430c8\nslug: ops\nname: Ops\n")
	write("teams/ops/rules/pager.md", "Not this project's team.\n")
	if projectDeclaresReact {
		write("projects/shop/rules/react.md", "React the shop way.\n")
	}
	write("users/danidani23/rules/mine.md", "My rule.\n")
	write("users/danidani23/rules/security.md", "My security, which may not replace the required one.\n")
	return root
}

func addresses(chosen []Chosen) string {
	var list []string
	for _, item := range chosen {
		list = append(list, item.Address)
	}
	return strings.Join(list, "\n")
}

func TestResolve(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	t.Run("selectors, skips with an exception, replacement, and the problems", func(t *testing.T) {
		root := orgClone(t, false, false)
		tree, _, err := declaration.LoadTree(root)
		if err != nil {
			t.Fatalf("tree: %v", err)
		}
		declarations, err := LoadDeclarations(root)
		if err != nil || len(declarations) != 15 {
			t.Fatalf("declarations %d err %v", len(declarations), err)
		}
		context, found := ContextOf(tree, "shop", "danidani23", now)
		if !found || len(context.Skips) != 2 || len(context.Exceptions) != 1 {
			t.Fatalf("context %+v found %v", context, found)
		}
		result := Resolve(declarations, context)
		// nodocs and style are skipped, react is contested, py and pager do not apply
		expected := []string{
			"organizations.acme.mcpservers.github",
			"organizations.acme.rules.go",
			"organizations.acme.rules.security",
			"organizations.acme.skills.commit",
			"organizations.acme.teams.web.rules.testing",
			"organizations.acme.users.danidani23.rules.mine",
		}
		if addresses(result.Chosen) != strings.Join(expected, "\n") {
			t.Fatalf("chosen:\n%s", addresses(result.Chosen))
		}
		for _, item := range result.Chosen {
			if item.Address == "organizations.acme.teams.web.rules.testing" && strings.Join(item.Replaced, ",") != "organizations.acme.rules.testing" {
				t.Fatalf("testing replaced %v", item.Replaced)
			}
		}
		if len(result.Skipped) != 2 || result.Skipped[0].Address != "organizations.acme.rules.nodocs" || result.Skipped[0].By != "organizations.acme.teams.web" || result.Skipped[1].Exception != "organizations.acme.exceptions.data-style" {
			t.Fatalf("skipped %+v", result.Skipped)
		}
		var messages []string
		for _, problem := range result.Problems {
			messages = append(messages, problem.Address+": "+problem.Message)
		}
		joined := strings.Join(messages, "\n")
		for _, expected := range []string{"organizations.acme.teams.data.rules.react: the same rules react is declared by organizations.acme.teams.web.rules.react", "organizations.acme.teams.web.rules.react: the same rules react is declared by organizations.acme.teams.data.rules.react", "organizations.acme.teams.web.mcpservers.github: redeclares organizations.acme.mcpservers.github, which is required", "organizations.acme.users.danidani23.rules.security: redeclares organizations.acme.rules.security, which is required"} {
			if !strings.Contains(joined, expected) {
				t.Fatalf("missing %q in:\n%s", expected, joined)
			}
		}
		if len(result.Problems) != 4 {
			t.Fatalf("problems:\n%s", joined)
		}
	})
	t.Run("the project settles two teams' ids, and an expired exception leaves the required rule in place with a problem", func(t *testing.T) {
		root := orgClone(t, true, true)
		tree, _, _ := declaration.LoadTree(root)
		declarations, _ := LoadDeclarations(root)
		context, _ := ContextOf(tree, "shop", "", now)
		result := Resolve(declarations, context)
		chosen := addresses(result.Chosen)
		if !strings.Contains(chosen, "organizations.acme.projects.shop.rules.react") || strings.Contains(chosen, "teams.web.rules.react") || strings.Contains(chosen, "users.") {
			t.Fatalf("chosen:\n%s", chosen)
		}
		var messages []string
		for _, problem := range result.Problems {
			messages = append(messages, problem.Address+": "+problem.Message)
		}
		joined := strings.Join(messages, "\n")
		if !strings.Contains(joined, "organizations.acme.rules.style: organizations.acme.teams.data skips it, but it is required") || strings.Contains(joined, "react") {
			t.Fatalf("problems:\n%s", joined)
		}
		if len(result.Skipped) != 1 {
			t.Fatalf("skipped %+v", result.Skipped)
		}
	})
	t.Run("an unknown project has no context", func(t *testing.T) {
		root := orgClone(t, false, false)
		tree, _, _ := declaration.LoadTree(root)
		_, found := ContextOf(tree, "nope", "", now)
		if found {
			t.Fatalf("found a project that does not exist")
		}
	})
}
