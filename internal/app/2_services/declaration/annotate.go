package declaration

import (
	"fmt"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

// AnnotateApplyIf returns a candidate's files with applyIf set on its
// declaration: in a rule's frontmatter, in a skill's metadata under
// rein-apply-if, and at the top of a hook, a permission set or a tool
// server. Without labels the files come back as they are.
func AnnotateApplyIf(kind string, files map[string]string, applyIf []string) (map[string]string, error) {
	if len(applyIf) == 0 {
		return files, nil
	}
	annotated := map[string]string{}
	for relative, content := range files {
		annotated[relative] = content
	}
	for relative, content := range files {
		base := path.Base(relative)
		var err error
		switch {
		case kind == "rules" && strings.HasSuffix(base, ".md"):
			annotated[relative], err = withFrontmatter(content, func(mapping map[string]any) { mapping["applyIf"] = applyIf })
		case kind == "skills" && base == "SKILL.md":
			annotated[relative], err = withFrontmatter(content, func(mapping map[string]any) {
				metadata, _ := mapping["metadata"].(map[string]any)
				if metadata == nil {
					metadata = map[string]any{}
				}
				metadata[SkillApplyIfKey] = strings.Join(applyIf, " ")
				mapping["metadata"] = metadata
			})
		case (kind == "hooks" && base == "hook.yaml") || ((kind == "permissions" || kind == "mcpservers") && strings.HasSuffix(base, ".yaml")):
			annotated[relative], err = withTopLevelField(content, "applyIf", applyIf)
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", relative, err)
		}
	}
	return annotated, nil
}

// withFrontmatter rewrites a Markdown file's frontmatter through set,
// adding one when the file has none.
func withFrontmatter(content string, set func(mapping map[string]any)) (string, error) {
	frontmatter, body, found, err := SplitFrontmatter(content)
	if err != nil {
		return "", err
	}
	mapping := map[string]any{}
	if found {
		mapping, err = parseMapping(frontmatter)
		if err != nil {
			return "", err
		}
	} else {
		body = "\n" + body
	}
	set(mapping)
	encoded, err := yaml.Marshal(mapping)
	if err != nil {
		return "", err
	}
	return "---\n" + string(encoded) + "---\n" + body, nil
}

// withTopLevelField sets one field of a YAML mapping file.
func withTopLevelField(content string, field string, value any) (string, error) {
	mapping := map[string]any{}
	err := yaml.Unmarshal([]byte(content), &mapping)
	if err != nil {
		return "", fmt.Errorf("not a YAML mapping: %w", err)
	}
	mapping[field] = value
	encoded, err := yaml.Marshal(mapping)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
