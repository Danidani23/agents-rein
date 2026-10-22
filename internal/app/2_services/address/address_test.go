package address

import "testing"

func TestParse(t *testing.T) {
	t.Run("a skill under a team parses and renders back", func(t *testing.T) {
		parsed, err := Parse("organizations.dani.teams.web.skills.commit")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if parsed.String() != "organizations.dani.teams.web.skills.commit" {
			t.Fatalf("round trip changed the address: %s", parsed)
		}
		if parsed.Kind() != Skills || parsed.ID() != "commit" {
			t.Fatalf("kind %q id %q", parsed.Kind(), parsed.ID())
		}
		if !parsed.IsDeclaration() {
			t.Fatalf("a skill is a declaration")
		}
	})
	t.Run("a user's rule parses", func(t *testing.T) {
		_, err := Parse("organizations.dani.users.danielmolnar.rules.communication")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("the parent of a declaration is its level", func(t *testing.T) {
		parsed, _ := Parse("organizations.dani.projects.w2going.rules.api")
		parent, found := parsed.Parent()
		if !found || parent.String() != "organizations.dani.projects.w2going" {
			t.Fatalf("parent %q found %v", parent, found)
		}
		if parent.IsDeclaration() {
			t.Fatalf("a project is not a declaration")
		}
	})
	t.Run("an odd number of segments is refused", func(t *testing.T) {
		_, err := Parse("organizations.dani.skills")
		if err == nil {
			t.Fatalf("expected an error")
		}
	})
	t.Run("an address must start at organizations or users", func(t *testing.T) {
		_, err := Parse("teams.web.skills.commit")
		if err == nil {
			t.Fatalf("expected an error")
		}
	})
	t.Run("labels live at the organisation level only", func(t *testing.T) {
		_, err := Parse("organizations.dani.teams.web.labels.language")
		if err == nil {
			t.Fatalf("expected an error")
		}
	})
	t.Run("nothing nests below a declaration", func(t *testing.T) {
		_, err := Parse("organizations.dani.skills.commit.rules.x")
		if err == nil {
			t.Fatalf("expected an error")
		}
	})
	t.Run("a slug with uppercase or a double hyphen is refused", func(t *testing.T) {
		for _, text := range []string{"organizations.Dani", "organizations.da--ni", "organizations.-dani", "organizations.dani-"} {
			_, err := Parse(text)
			if err == nil {
				t.Fatalf("expected an error for %q", text)
			}
		}
	})
	t.Run("an empty address is refused", func(t *testing.T) {
		_, err := Parse("")
		if err == nil {
			t.Fatalf("expected an error")
		}
	})
}

func TestFromPath(t *testing.T) {
	t.Run("a directory path becomes an address", func(t *testing.T) {
		parsed, err := FromPath("projects/w2going/skills/deploy", "dani")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if parsed.String() != "organizations.dani.projects.w2going.skills.deploy" {
			t.Fatalf("got %s", parsed)
		}
	})
	t.Run("a path that is not pairs is refused", func(t *testing.T) {
		_, err := FromPath("skills", "dani")
		if err == nil {
			t.Fatalf("expected an error")
		}
	})
}
