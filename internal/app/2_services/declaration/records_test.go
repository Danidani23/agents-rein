package declaration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	validTeam    = "id: 1b4e28ba-2fa1-11d2-883f-0016d3cca427\nslug: web\nname: Web team\n"
	validProject = "id: 6ba7b810-9dad-11d1-80b4-00c04fd430c8\nslug: shop\nname: Shop\nteams: [web]\nlabels: [language:go, kind:web-app]\n"
	validUser    = "slug: danidani23\ngithub: Danidani23\nname: Daniel\n"
	validLabels  = "category: language\nlabels:\n  go: Go\n  dart: Dart\n"
)

func expectProblem(t *testing.T, problems []Problem, expected string) {
	t.Helper()
	for _, problem := range problems {
		if strings.Contains(problem.Message, expected) {
			return
		}
	}
	t.Fatalf("problems %v, expected one containing %q", problems, expected)
}

func TestParseTeam(t *testing.T) {
	t.Run("a valid team parses", func(t *testing.T) {
		team, problems := ParseTeam("web", []byte(validTeam))
		if len(problems) != 0 || team.Name != "Web team" {
			t.Fatalf("team %+v problems %v", team, problems)
		}
	})
	t.Run("a bad id, a mismatched slug, a missing name and an unknown field are refused", func(t *testing.T) {
		_, problems := ParseTeam("other", []byte("id: nope\nslug: web\ncolour: blue\n"))
		expectProblem(t, problems, "colour")
		_, problems = ParseTeam("other", []byte("id: nope\nslug: web\n"))
		expectProblem(t, problems, "not a lowercase UUID")
		expectProblem(t, problems, "does not match the folder")
		expectProblem(t, problems, "name is required")
	})
}

func TestParseProject(t *testing.T) {
	t.Run("a valid project parses with the root path by default", func(t *testing.T) {
		project, problems := ParseProject("shop", []byte(validProject))
		if len(problems) != 0 || project.Path != "." || len(project.Teams) != 1 || len(project.Labels) != 2 {
			t.Fatalf("project %+v problems %v", project, problems)
		}
	})
	t.Run("a project needs a team, well-formed labels, a relative path and real hashes", func(t *testing.T) {
		content := "id: 6ba7b810-9dad-11d1-80b4-00c04fd430c8\nslug: shop\nname: Shop\nteams: []\nlabels: [nocolon]\npath: ../out\nfingerprint: [abc]\n"
		_, problems := ParseProject("shop", []byte(content))
		expectProblem(t, problems, "at least one team")
		expectProblem(t, problems, "not category:value")
		expectProblem(t, problems, "relative path inside the repository")
		expectProblem(t, problems, "not a commit hash")
	})
	t.Run("a subdirectory path is cleaned", func(t *testing.T) {
		project, problems := ParseProject("shop", []byte(validProject+"path: ./services/shop/\n"))
		if len(problems) != 0 || project.Path != "services/shop" {
			t.Fatalf("project %+v problems %v", project, problems)
		}
	})
}

func TestParseUser(t *testing.T) {
	t.Run("a valid user parses", func(t *testing.T) {
		user, problems := ParseUser("danidani23", []byte(validUser))
		if len(problems) != 0 || user.GitHub != "Danidani23" {
			t.Fatalf("user %+v problems %v", user, problems)
		}
	})
	t.Run("the slug must be the lowercase login and the folder", func(t *testing.T) {
		_, problems := ParseUser("danidani23", []byte("slug: danidani23\ngithub: Someone\n"))
		expectProblem(t, problems, "lowercase form of the github login")
		_, problems = ParseUser("danidani23", []byte("slug: danidani23\n"))
		expectProblem(t, problems, "github is required")
	})
}

func TestParseLabelCategory(t *testing.T) {
	t.Run("a valid category parses and lists its values sorted", func(t *testing.T) {
		category, problems := ParseLabelCategory("language", []byte(validLabels))
		if len(problems) != 0 || strings.Join(category.LabelValues(), ",") != "dart,go" {
			t.Fatalf("category %+v problems %v", category, problems)
		}
	})
	t.Run("the category must match its file name and hold slugs", func(t *testing.T) {
		_, problems := ParseLabelCategory("kind", []byte("category: language\nlabels:\n  Go Lang: x\n"))
		expectProblem(t, problems, "does not match the file name")
		expectProblem(t, problems, "not a slug")
	})
	t.Run("the seeded registry is valid", func(t *testing.T) {
		for _, category := range SeededLabels() {
			if len(category.Labels) == 0 || !strings.Contains("language kind data stack", category.Category) {
				t.Fatalf("seeded %+v", category)
			}
			// what a project uses takes several values, OpenTofu and Terraform one of them (D40)
			if category.Category == "stack" && (!category.Multiple || !strings.Contains(category.Labels["opentofu"], "Terraform")) {
				t.Fatalf("stack %+v", category)
			}
		}
	})
}

func writeTreeFile(t *testing.T, root string, relative string, content string) {
	t.Helper()
	full := filepath.Join(root, relative)
	err := os.MkdirAll(filepath.Dir(full), 0o755)
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	err = os.WriteFile(full, []byte(content), 0o644)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
}

func scratchTree(t *testing.T) string {
	t.Helper()
	clone := t.TempDir()
	writeTreeFile(t, clone, "declarations/organization.yaml", validOrganization)
	writeTreeFile(t, clone, "declarations/labels/language.yaml", validLabels)
	writeTreeFile(t, clone, "declarations/labels/kind.yaml", "category: kind\nlabels:\n  web-app: a web application\n")
	writeTreeFile(t, clone, "declarations/teams/web/team.yaml", validTeam)
	writeTreeFile(t, clone, "declarations/projects/shop/project.yaml", validProject)
	writeTreeFile(t, clone, "declarations/users/danidani23/user.yaml", validUser)
	return clone
}

func problemsOf(problems []TreeProblem) []string {
	var all []string
	for _, problem := range problems {
		all = append(all, problem.Address+": "+problem.Problem.Message)
	}
	return all
}

func TestLoadTree(t *testing.T) {
	t.Run("a consistent tree loads without problems", func(t *testing.T) {
		tree, problems, err := LoadTree(scratchTree(t))
		if err != nil || len(problems) != 0 {
			t.Fatalf("err %v problems %v", err, problemsOf(problems))
		}
		if len(tree.Teams) != 1 || len(tree.Projects) != 1 || len(tree.Users) != 1 || len(tree.Labels) != 2 {
			t.Fatalf("tree %+v", tree)
		}
	})
	t.Run("a missing team, an unregistered label and a missing category are reported at the project", func(t *testing.T) {
		clone := scratchTree(t)
		writeTreeFile(t, clone, "declarations/projects/shop/project.yaml", strings.Replace(validProject, "teams: [web]\nlabels: [language:go, kind:web-app]", "teams: [mobile]\nlabels: [language:cobol, client:acme]", 1))
		_, problems, err := LoadTree(clone)
		if err != nil {
			t.Fatalf("err %v", err)
		}
		text := strings.Join(problemsOf(problems), "\n")
		for _, expected := range []string{"organizations.acme.projects.shop: team \"mobile\" does not exist", "label \"language:cobol\" is not in the registry; language has dart, go", "label \"client:acme\" names no category"} {
			if !strings.Contains(text, expected) {
				t.Fatalf("missing %q in %q", expected, text)
			}
		}
	})
	t.Run("two records with one id and two projects with one repository and path are reported", func(t *testing.T) {
		clone := scratchTree(t)
		hash := strings.Repeat("a", 40)
		writeTreeFile(t, clone, "declarations/projects/shop/project.yaml", validProject+"fingerprint: ["+hash+"]\n")
		writeTreeFile(t, clone, "declarations/projects/store/project.yaml", strings.Replace(validProject, "slug: shop", "slug: store", 1)+"fingerprint: ["+hash+"]\n")
		_, problems, _ := LoadTree(clone)
		text := strings.Join(problemsOf(problems), "\n")
		if !strings.Contains(text, "is also the id of organizations.acme.projects.shop") || !strings.Contains(text, "the same repository and path as organizations.acme.projects.shop") {
			t.Fatalf("problems %q", text)
		}
	})
	t.Run("a broken record is reported at its address and left out of the tree", func(t *testing.T) {
		clone := scratchTree(t)
		writeTreeFile(t, clone, "declarations/teams/web/team.yaml", "id: nope\nslug: web\nname: Web\n")
		tree, problems, _ := LoadTree(clone)
		text := strings.Join(problemsOf(problems), "\n")
		if !strings.Contains(text, "organizations.acme.teams.web: id \"nope\"") || len(tree.Teams) != 0 {
			t.Fatalf("problems %q teams %v", text, tree.Teams)
		}
	})
	t.Run("an organisation record with a problem still lets the tree cross-check", func(t *testing.T) {
		clone := scratchTree(t)
		writeTreeFile(t, clone, "declarations/organization.yaml", strings.Replace(validOrganization, "host: github", "host: gitlab", 1))
		writeTreeFile(t, clone, "declarations/projects/shop/project.yaml", strings.Replace(validProject, "teams: [web]", "teams: [mobile]", 1))
		_, problems, err := LoadTree(clone)
		if err != nil {
			t.Fatalf("err %v", err)
		}
		text := strings.Join(problemsOf(problems), "\n")
		if !strings.Contains(text, "organizations.acme: host \"gitlab\"") || !strings.Contains(text, "team \"mobile\" does not exist") {
			t.Fatalf("problems %q", text)
		}
	})
	t.Run("without an organisation the tree is an error", func(t *testing.T) {
		_, _, err := LoadTree(t.TempDir())
		if err == nil {
			t.Fatalf("expected an error")
		}
	})
}

func TestRequiredLabelCategories(t *testing.T) {
	category, problems := ParseLabelCategory("kind", []byte("category: kind\nrequired: true\nlabels:\n  cli: a command-line tool\n"))
	if len(problems) != 0 || !category.Required {
		t.Fatalf("category %+v problems %v", category, problems)
	}
	registry := map[string]LabelCategory{"kind": {Category: "kind", Required: true}, "language": {Category: "language"}, "data": {Category: "data", Required: true}}
	if missing := MissingRequiredCategories(registry, []string{"language:go", "data:public"}); strings.Join(missing, ",") != "kind" {
		t.Fatalf("missing %v", missing)
	}
	if missing := MissingRequiredCategories(registry, []string{"kind:cli", "data:public", "broken"}); len(missing) != 0 {
		t.Fatalf("missing %v", missing)
	}
	registry["language"] = LabelCategory{Category: "language", Multiple: true}
	conflicts := SingleValueConflicts(registry, []string{"data:internal", "data:public", "language:go", "language:rust", "kind:cli", "unknown:a", "unknown:b"})
	if strings.Join(conflicts, "|") != "data takes one value; given internal, public" {
		t.Fatalf("conflicts %v", conflicts)
	}
}
