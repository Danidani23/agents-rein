package declaration

import (
	"strings"
	"testing"
)

func TestSplitFrontmatter(t *testing.T) {
	t.Run("a fenced block is separated from the body", func(t *testing.T) {
		frontmatter, body, found, err := SplitFrontmatter("---\nname: x\n---\n# Title\n")
		if err != nil || !found {
			t.Fatalf("err %v found %v", err, found)
		}
		if frontmatter != "name: x" || body != "# Title\n" {
			t.Fatalf("frontmatter %q body %q", frontmatter, body)
		}
	})
	t.Run("a file without a fence has no frontmatter", func(t *testing.T) {
		_, body, found, err := SplitFrontmatter("# Title\n")
		if err != nil || found || body != "# Title\n" {
			t.Fatalf("err %v found %v body %q", err, found, body)
		}
	})
	t.Run("an unclosed fence is an error", func(t *testing.T) {
		_, _, _, err := SplitFrontmatter("---\nname: x\n")
		if err == nil {
			t.Fatalf("expected an error")
		}
	})
}

func TestParseSkill(t *testing.T) {
	t.Run("the specification fields are read", func(t *testing.T) {
		content := "---\nname: commit\ndescription: Draft commits.\nlicense: MIT\nmetadata:\n  author: dani\n  version: \"1.0\"\nallowed-tools: Bash(git:*)\n---\nbody\n"
		skill, problems, parsed := ParseSkill("commit/SKILL.md", "commit", []byte(content))
		if !parsed || len(problems) != 0 {
			t.Fatalf("parsed %v problems %v", parsed, problems)
		}
		if skill.Name != "commit" || skill.License != "MIT" || skill.Metadata["version"] != "1.0" || skill.AllowedTools != "Bash(git:*)" {
			t.Fatalf("fields not read: %+v", skill)
		}
		if len(skill.Validate()) != 0 {
			t.Fatalf("unexpected validation problems: %v", skill.Validate())
		}
	})
	t.Run("a field outside the specification is an error", func(t *testing.T) {
		content := "---\nname: adr\ndescription: Write an ADR.\nargument-hint: \"<short-description>\"\n---\n"
		_, problems, parsed := ParseSkill("adr/SKILL.md", "adr", []byte(content))
		if !parsed || len(problems) != 1 || !strings.Contains(problems[0].Message, "argument-hint") {
			t.Fatalf("parsed %v problems %v", parsed, problems)
		}
	})
	t.Run("a metadata value that is not a string is an error", func(t *testing.T) {
		content := "---\nname: adr\ndescription: Write an ADR.\nmetadata:\n  version: 1.0\n---\n"
		_, problems, _ := ParseSkill("adr/SKILL.md", "adr", []byte(content))
		if len(problems) != 1 || !strings.Contains(problems[0].Message, "metadata value") {
			t.Fatalf("problems %v", problems)
		}
	})
	t.Run("a file without frontmatter yields one problem and no parse", func(t *testing.T) {
		_, problems, parsed := ParseSkill("x/SKILL.md", "x", []byte("# Just a body\n"))
		if parsed || len(problems) != 1 {
			t.Fatalf("parsed %v problems %v", parsed, problems)
		}
	})
}

func TestRemoveFieldsOutsideSpecification(t *testing.T) {
	t.Run("a field outside the specification goes with all its lines, the rest stays as written", func(t *testing.T) {
		content := "---\nname: adr\ndescription: Write an ADR.\nargument-hint: \"<short-description>\"\nhooks:\n  PreToolUse:\n    - matcher: Bash\n\n# the license\nlicense: MIT\n---\n# ADR\n"
		kept, removed := RemoveFieldsOutsideSpecification(content)
		if kept != "---\nname: adr\ndescription: Write an ADR.\n\n# the license\nlicense: MIT\n---\n# ADR\n" {
			t.Fatalf("kept %q", kept)
		}
		if len(removed) != 2 || removed[0] != `argument-hint: "<short-description>"` || removed[1] != "hooks: PreToolUse: - matcher: Bash" {
			t.Fatalf("removed %q", removed)
		}
	})
	t.Run("the specification's fields alone, no frontmatter, or frontmatter that cannot be read stay as they are", func(t *testing.T) {
		for _, content := range []string{"---\nname: commit\ndescription: Draft commits.\n---\nbody\n", "# Just a body\n", "---\nname: [commit\n---\n", "---\n{name: commit, argument-hint: x}\n---\n"} {
			kept, removed := RemoveFieldsOutsideSpecification(content)
			if kept != content || len(removed) != 0 {
				t.Fatalf("content %q kept %q removed %q", content, kept, removed)
			}
		}
	})
}

func TestSkillValidate(t *testing.T) {
	cases := []struct {
		name     string
		skill    Skill
		expected string
	}{
		{"a missing name is required", Skill{Directory: "x", Description: "d"}, "name is required"},
		{"a name with uppercase is refused", Skill{Directory: "x", Name: "Commit", Description: "d"}, "lowercase"},
		{"a name must match the directory", Skill{Directory: "commit", Name: "commits", Description: "d"}, "does not match the directory"},
		{"a name over 64 characters is refused", Skill{Directory: "x", Name: strings.Repeat("a", 65), Description: "d"}, "maximum is 64"},
		{"a missing description is required", Skill{Directory: "x", Name: "x"}, "description is required"},
		{"a description over 1024 characters is refused", Skill{Directory: "x", Name: "x", Description: strings.Repeat("d", 1025)}, "maximum is 1024"},
		{"a compatibility over 500 characters is refused", Skill{Directory: "x", Name: "x", Description: "d", Compatibility: strings.Repeat("c", 501)}, "maximum is 500"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			problems := testCase.skill.Validate()
			if len(problems) != 1 || !strings.Contains(problems[0].Message, testCase.expected) {
				t.Fatalf("problems %v, expected one containing %q", problems, testCase.expected)
			}
		})
	}
}

func TestParseRule(t *testing.T) {
	t.Run("a paths list is read", func(t *testing.T) {
		rule, problems := ParseRule("rules/go.md", []byte("---\npaths:\n  - \"**/*.go\"\n---\n# Go\n"))
		if len(problems) != 0 || len(rule.Paths) != 1 || rule.Paths[0] != "**/*.go" {
			t.Fatalf("rule %+v problems %v", rule, problems)
		}
	})
	t.Run("a rule without frontmatter is unconditional", func(t *testing.T) {
		rule, problems := ParseRule("rules/git.md", []byte("# Git\n"))
		if len(problems) != 0 || rule.Paths != nil {
			t.Fatalf("rule %+v problems %v", rule, problems)
		}
	})
	t.Run("paths that is not a list is an error", func(t *testing.T) {
		_, problems := ParseRule("rules/go.md", []byte("---\npaths: \"**/*.go\"\n---\n"))
		if len(problems) != 1 || problems[0].Severity != SeverityError {
			t.Fatalf("problems %v", problems)
		}
	})
	t.Run("a field other than paths is a warning", func(t *testing.T) {
		_, problems := ParseRule("rules/go.md", []byte("---\nname: go\n---\n"))
		if len(problems) != 1 || problems[0].Severity != SeverityWarning {
			t.Fatalf("problems %v", problems)
		}
	})
}

func TestParseSettings(t *testing.T) {
	t.Run("allow and deny lists are read", func(t *testing.T) {
		settings, problems := ParseSettings("settings.json", []byte(`{"permissions":{"allow":["Bash(git *)"],"deny":["Bash(git push*)"]}}`))
		if len(problems) != 0 || len(settings.Permissions.Allow) != 1 || len(settings.Permissions.Deny) != 1 {
			t.Fatalf("settings %+v problems %v", settings, problems)
		}
	})
	t.Run("invalid JSON is an error", func(t *testing.T) {
		_, problems := ParseSettings("settings.json", []byte(`{"permissions":`))
		if len(problems) != 1 {
			t.Fatalf("problems %v", problems)
		}
	})
}
