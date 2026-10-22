package declaration

import (
	"strings"
	"testing"
)

func TestAnnotateApplyIf(t *testing.T) {
	labels := []string{"language:go", "kind:service"}
	rule, err := AnnotateApplyIf("rules", map[string]string{"rules/style.md": "---\npaths:\n  - \"**/*.go\"\n---\n\nTwo-line errors.\n"}, labels)
	if err != nil || !strings.Contains(rule["rules/style.md"], "applyIf:\n    - language:go\n    - kind:service\n") || !strings.Contains(rule["rules/style.md"], "paths:\n    - '**/*.go'\n") || !strings.HasSuffix(rule["rules/style.md"], "---\n\nTwo-line errors.\n") {
		t.Fatalf("rule %q err %v", rule["rules/style.md"], err)
	}
	bare, err := AnnotateApplyIf("rules", map[string]string{"rules/brief.md": "Be brief.\n"}, labels)
	if err != nil || bare["rules/brief.md"] != "---\napplyIf:\n    - language:go\n    - kind:service\n---\n\nBe brief.\n" {
		t.Fatalf("bare %q err %v", bare["rules/brief.md"], err)
	}
	skill, err := AnnotateApplyIf("skills", map[string]string{"skills/go/SKILL.md": "---\nname: go\ndescription: Go style\nmetadata:\n  owner: web\n---\n\n# Go\n", "skills/go/notes.md": "notes"}, labels)
	parsed, problems, _ := ParseSkill("skills/go/SKILL.md", "go", []byte(skill["skills/go/SKILL.md"]))
	if err != nil || len(problems) != 0 || parsed.Metadata[SkillApplyIfKey] != "language:go kind:service" || parsed.Metadata["owner"] != "web" || skill["skills/go/notes.md"] != "notes" {
		t.Fatalf("skill %q problems %v err %v", skill["skills/go/SKILL.md"], problems, err)
	}
	hook, err := AnnotateApplyIf("hooks", map[string]string{"hooks/fmt/hook.yaml": "event: PostToolUse\nrun: run.sh\n", "hooks/fmt/run.sh": "#!/bin/sh\n"}, labels)
	if err != nil || !strings.Contains(hook["hooks/fmt/hook.yaml"], "applyIf:\n    - language:go\n") || !strings.Contains(hook["hooks/fmt/hook.yaml"], "event: PostToolUse\n") || hook["hooks/fmt/run.sh"] != "#!/bin/sh\n" {
		t.Fatalf("hook %q err %v", hook["hooks/fmt/hook.yaml"], err)
	}
	same, err := AnnotateApplyIf("rules", map[string]string{"rules/brief.md": "Be brief.\n"}, nil)
	if err != nil || same["rules/brief.md"] != "Be brief.\n" {
		t.Fatalf("same %q err %v", same, err)
	}
	_, err = AnnotateApplyIf("permissions", map[string]string{"permissions/x.yaml": "- not\n- a mapping\n"}, labels)
	if err == nil {
		t.Fatalf("a sequence passed as a mapping")
	}
}
