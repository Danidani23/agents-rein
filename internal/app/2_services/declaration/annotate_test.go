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

func TestSelectorsOfAndSetSelectors(t *testing.T) {
	labelled := Selectors{ApplyIf: []string{"language:go"}, DoNotApplyIf: []string{"data:regulated"}, Enforcement: EnforcementRequired}
	rule, err := SetSelectors("rules", map[string]string{"rules/style.md": "---\npaths:\n  - \"**/*.go\"\napplyIf:\n  - old:label\n---\n\nTwo-line errors.\n"}, labelled)
	read, problems := SelectorsOf("rules", rule)
	if err != nil || len(problems) != 0 || strings.Join(read.ApplyIf, ",") != "language:go" || strings.Join(read.DoNotApplyIf, ",") != "data:regulated" || read.Enforcement != "required" || !strings.Contains(rule["rules/style.md"], "paths:") || strings.Contains(rule["rules/style.md"], "old:label") {
		t.Fatalf("rule %q read %+v problems %v err %v", rule, read, problems, err)
	}
	bare, err := SetSelectors("rules", map[string]string{"rules/brief.md": "---\napplyIf:\n  - language:go\n---\n\nBe brief.\n"}, Selectors{})
	if err != nil || bare["rules/brief.md"] != "Be brief.\n" {
		t.Fatalf("bare %q err %v", bare["rules/brief.md"], err)
	}
	skill, err := SetSelectors("skills", map[string]string{"skills/go/SKILL.md": "---\nname: go\ndescription: Go style\nmetadata:\n  rein-apply-if: language:rust\n  owner: web\n---\n\n# Go\n"}, Selectors{ApplyIf: []string{"language:go"}})
	read, problems = SelectorsOf("skills", skill)
	if err != nil || len(problems) != 0 || strings.Join(read.ApplyIf, ",") != "language:go" || !strings.Contains(skill["skills/go/SKILL.md"], "owner: web") || strings.Contains(skill["skills/go/SKILL.md"], "language:rust") {
		t.Fatalf("skill %q read %+v err %v", skill, read, err)
	}
	cleared, _ := SetSelectors("skills", map[string]string{"skills/go/SKILL.md": "---\nname: go\ndescription: Go style\nmetadata:\n  rein-apply-if: language:go\n---\n\n# Go\n"}, Selectors{})
	if strings.Contains(cleared["skills/go/SKILL.md"], "metadata") {
		t.Fatalf("cleared %q", cleared)
	}
	hook, err := SetSelectors("hooks", map[string]string{"hooks/fmt/hook.yaml": "event: PostToolUse\nrun: run.sh\napplyIf:\n  - old:label\n", "hooks/fmt/run.sh": "#!/bin/sh\n"}, Selectors{Enforcement: EnforcementDefault})
	read, problems = SelectorsOf("hooks", hook)
	if err != nil || len(problems) != 0 || len(read.ApplyIf) != 0 || read.Enforcement != "default" || !strings.Contains(hook["hooks/fmt/hook.yaml"], "event: PostToolUse") {
		t.Fatalf("hook %q read %+v err %v", hook, read, err)
	}
}
