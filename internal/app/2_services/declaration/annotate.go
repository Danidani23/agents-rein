package declaration

import (
	"fmt"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

// AnnotateApplyIf returns the candidate's files with applyIf set on its
// declaration; see AnnotateSelectors.
func AnnotateApplyIf(kind string, files map[string]string, applyIf []string) (map[string]string, error) {
	return AnnotateSelectors(kind, files, Selectors{ApplyIf: applyIf})
}

// AnnotateSelectors returns a candidate's files with the selectors set on
// its declaration: in a rule's frontmatter, in a skill's metadata under the
// rein- keys, and at the top of a hook, a permission set or a tool server.
// An empty selector is left out, so files without any come back as they
// are.
func AnnotateSelectors(kind string, files map[string]string, selectors Selectors) (map[string]string, error) {
	if len(selectors.ApplyIf) == 0 && len(selectors.DoNotApplyIf) == 0 && selectors.Enforcement == "" {
		return files, nil
	}
	fields := map[string]any{}
	metadata := map[string]any{}
	if len(selectors.ApplyIf) > 0 {
		fields["applyIf"] = selectors.ApplyIf
		metadata[SkillApplyIfKey] = strings.Join(selectors.ApplyIf, " ")
	}
	if len(selectors.DoNotApplyIf) > 0 {
		fields["doNotApplyIf"] = selectors.DoNotApplyIf
		metadata[SkillDoNotApplyIfKey] = strings.Join(selectors.DoNotApplyIf, " ")
	}
	if selectors.Enforcement != "" {
		fields["enforcement"] = selectors.Enforcement
		metadata[SkillEnforcementKey] = selectors.Enforcement
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
			annotated[relative], err = withFrontmatter(content, func(mapping map[string]any) {
				for field, value := range fields {
					mapping[field] = value
				}
			})
		case kind == "skills" && base == "SKILL.md":
			annotated[relative], err = withFrontmatter(content, func(mapping map[string]any) {
				existing, _ := mapping["metadata"].(map[string]any)
				if existing == nil {
					existing = map[string]any{}
				}
				for key, value := range metadata {
					existing[key] = value
				}
				mapping["metadata"] = existing
			})
		case (kind == "hooks" && base == "hook.yaml") || ((kind == "permissions" || kind == "mcpservers") && strings.HasSuffix(base, ".yaml")):
			annotated[relative], err = withTopLevelFields(content, fields)
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
	if len(mapping) == 0 {
		return strings.TrimLeft(body, "\n"), nil
	}
	encoded, err := yaml.Marshal(mapping)
	if err != nil {
		return "", err
	}
	return "---\n" + string(encoded) + "---\n" + body, nil
}

// withTopLevelFields sets fields of a YAML mapping file.
func withTopLevelFields(content string, fields map[string]any) (string, error) {
	mapping := map[string]any{}
	err := yaml.Unmarshal([]byte(content), &mapping)
	if err != nil {
		return "", fmt.Errorf("not a YAML mapping: %w", err)
	}
	for field, value := range fields {
		mapping[field] = value
	}
	encoded, err := yaml.Marshal(mapping)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// SelectorsOf reads a declaration's selectors from its files: a rule's
// frontmatter, a skill's metadata, the top of a hook, a permission set or a
// tool server.
func SelectorsOf(kind string, files map[string]string) (Selectors, []Problem) {
	for relative, content := range files {
		base := path.Base(relative)
		switch {
		case kind == "rules" && strings.HasSuffix(base, ".md"):
			_, selectors, problems := ParseRuleDeclaration(relative, []byte(content))
			return selectors, problems
		case kind == "skills" && base == "SKILL.md":
			skill, problems, parsed := ParseSkill(relative, path.Base(path.Dir(relative)), []byte(content))
			if !parsed {
				return Selectors{}, problems
			}
			selectors, more := SkillSelectors(skill)
			return selectors, append(problems, more...)
		case (kind == "hooks" && base == "hook.yaml") || ((kind == "permissions" || kind == "mcpservers") && strings.HasSuffix(base, ".yaml")):
			var holder struct {
				Selectors `yaml:",inline"`
			}
			err := yaml.Unmarshal([]byte(content), &holder)
			if err != nil {
				return Selectors{}, []Problem{{Severity: SeverityError, Message: fmt.Sprintf("%s: not a YAML mapping: %v", relative, err)}}
			}
			return holder.Selectors, nil
		}
	}
	return Selectors{}, nil
}

// SetSelectors returns a declaration's files with exactly these selectors:
// a selector that is empty is taken out of the file.
func SetSelectors(kind string, files map[string]string, selectors Selectors) (map[string]string, error) {
	fields := map[string]any{}
	metadata := map[string]any{}
	if len(selectors.ApplyIf) > 0 {
		fields["applyIf"] = selectors.ApplyIf
		metadata[SkillApplyIfKey] = strings.Join(selectors.ApplyIf, " ")
	}
	if len(selectors.DoNotApplyIf) > 0 {
		fields["doNotApplyIf"] = selectors.DoNotApplyIf
		metadata[SkillDoNotApplyIfKey] = strings.Join(selectors.DoNotApplyIf, " ")
	}
	if selectors.Enforcement != "" {
		fields["enforcement"] = selectors.Enforcement
		metadata[SkillEnforcementKey] = selectors.Enforcement
	}
	setFields := func(mapping map[string]any) {
		for _, field := range []string{"applyIf", "doNotApplyIf", "enforcement"} {
			delete(mapping, field)
		}
		for field, value := range fields {
			mapping[field] = value
		}
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
			annotated[relative], err = withFrontmatter(content, setFields)
		case kind == "skills" && base == "SKILL.md":
			annotated[relative], err = withFrontmatter(content, func(mapping map[string]any) {
				existing, _ := mapping["metadata"].(map[string]any)
				if existing == nil {
					existing = map[string]any{}
				}
				for _, key := range []string{SkillApplyIfKey, SkillDoNotApplyIfKey, SkillEnforcementKey} {
					delete(existing, key)
				}
				for key, value := range metadata {
					existing[key] = value
				}
				if len(existing) == 0 {
					delete(mapping, "metadata")
				} else {
					mapping["metadata"] = existing
				}
			})
		case (kind == "hooks" && base == "hook.yaml") || ((kind == "permissions" || kind == "mcpservers") && strings.HasSuffix(base, ".yaml")):
			annotated[relative], err = withTopLevelFieldsExactly(content, setFields)
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", relative, err)
		}
	}
	return annotated, nil
}

// withTopLevelFieldsExactly rewrites a YAML mapping file through set.
func withTopLevelFieldsExactly(content string, set func(mapping map[string]any)) (string, error) {
	mapping := map[string]any{}
	err := yaml.Unmarshal([]byte(content), &mapping)
	if err != nil {
		return "", fmt.Errorf("not a YAML mapping: %w", err)
	}
	set(mapping)
	encoded, err := yaml.Marshal(mapping)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
