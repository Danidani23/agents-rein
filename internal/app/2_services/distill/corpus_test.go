package distill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCorpus(t *testing.T) {
	root := t.TempDir()
	write := func(relative string, content string) {
		target := filepath.Join(root, "declarations", filepath.FromSlash(relative))
		_ = os.MkdirAll(filepath.Dir(target), 0o755)
		_ = os.WriteFile(target, []byte(content), 0o644)
	}
	write("rules/style.md", "House style for everyone.")
	write("teams/web/rules/react.md", "React components stay small.")
	write("teams/data/rules/testing.md", "Tests are table-driven tests, always; coverage matters.")
	write("teams/data/rules/notebooks.md", "Notebooks never land on main.")
	write("teams/ops/mcpservers/pager.yaml", "description: pager\napplyIf: [kind:service]\ntransport: http\nurl: https://pager\n")
	write("projects/shop/skills/commit/SKILL.md", "---\nname: commit\n---\nDraft commits.")
	write("projects/shop/skills/commit/extra.md", "more")
	write("users/danidani23/rules/mine.md", "My own rule.")
	candidates := []Candidate{
		{Kind: "rules", ID: "go-testing", Files: map[string]string{"rules/go-testing.md": "Use table-driven tests only, and watch coverage."}},
		{Kind: "mcpservers", ID: "x", Files: map[string]string{"mcpservers/x.yaml": "applyIf: [kind:service]\ntransport: stdio\ncommand: x\n"}},
	}
	corpus, err := LoadCorpus(root, "organizations.acme.projects.shop", []string{"web"}, "danidani23", candidates)
	if err != nil {
		t.Fatalf("corpus: %v", err)
	}
	found := map[string]bool{}
	for _, passage := range corpus.Passages {
		found[passage.Address] = passage.Resolved
	}
	resolved, present := found["organizations.acme.rules.style"]
	if !present || !resolved {
		t.Fatalf("org rule: %v %v", present, resolved)
	}
	resolved, present = found["organizations.acme.teams.web.rules.react"]
	if !present || !resolved {
		t.Fatalf("team web rule: %v %v", present, resolved)
	}
	resolved, present = found["organizations.acme.projects.shop.skills.commit"]
	if !present || !resolved {
		t.Fatalf("project skill: %v %v", present, resolved)
	}
	resolved, present = found["organizations.acme.users.danidani23.rules.mine"]
	if !present || !resolved {
		t.Fatalf("user rule: %v %v", present, resolved)
	}
	resolved, present = found["organizations.acme.teams.data.rules.testing"]
	if !present || resolved {
		t.Fatalf("related other-team rule: %v %v", present, resolved)
	}
	resolved, present = found["organizations.acme.teams.ops.mcpservers.pager"]
	if !present || resolved {
		t.Fatalf("other-team server with a shared label: %v %v", present, resolved)
	}
	_, present = found["organizations.acme.teams.data.rules.notebooks"]
	if present {
		t.Fatalf("an unrelated other-team rule is in the corpus")
	}
	for _, passage := range corpus.Passages {
		if passage.Address == "organizations.acme.projects.shop.skills.commit" && (passage.Text == "" || !contains([]string{passage.Text}, passage.Text)) {
			t.Fatalf("skill text %q", passage.Text)
		}
		if passage.Address == "organizations.acme.teams.ops.mcpservers.pager" && (len(passage.ApplyIf) != 1 || passage.ApplyIf[0] != "kind:service") {
			t.Fatalf("server selectors %+v", passage)
		}
	}
}
