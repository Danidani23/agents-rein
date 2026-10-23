// Package compile turns a resolution into the files an agent reads, per
// agent (ADR, B3). The Claude Code emitter writes a CLAUDE.md that states
// the precedence and imports one generated file per level, the rules with
// paths under .claude/rules/, the skills and the hook scripts under .claude/,
// the permissions and the hooks into .claude/settings.local.json, merged
// with what the developer already allowed, and the tool servers into
// .mcp.json. Everything lands in the project clone (D21).
package compile

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Danidani23/agents-rein/internal/app/2_services/address"
	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/resolve"
)

// ReinFolder is where the generated level files live inside .claude/.
const ReinFolder = ".claude/rein"

// SettingsFile is the shared settings file: rein owns its hooks and its
// deny and ask lists, and merges the developer's own allow entries.
const SettingsFile = ".claude/settings.local.json"

// ReinServerID names rein's own tool server in .mcp.json: the queries as
// MCP tools, served by the executable the session-start hook calls.
const ReinServerID = "rein"

// Input is what the emitter needs besides the resolution.
type Input struct {
	OrganizationSlug string
	ProjectName      string
	ProjectSlug      string
	Teams            []string
	UserSlug         string
	// ReinExecutable is the absolute path the session-start hook calls.
	ReinExecutable string
	// ExistingSettings is the current settings.local.json, or empty.
	ExistingSettings string
}

// File is one generated file with the address it came from, "generated"
// for the files rein composes itself.
type File struct {
	Path    string
	Address string
	Content string
}

// Output is what the emitter produced.
type Output struct {
	Files []File
	// Variables are the REIN_ variables the tool servers reference.
	Variables []string
	// Notes say what the clone should know, such as a merged allow list.
	Notes []string
}

// levelFile names the generated file of a level.
func levelFile(level string, organization string) string {
	switch {
	case level == address.Organizations+"."+organization:
		return "organization"
	case strings.Contains(level, "."+address.Users+"."):
		return "user"
	case strings.Contains(level, ".teams."):
		return "team-" + level[strings.LastIndex(level, ".")+1:]
	default:
		return "project"
	}
}

// levelTitle names a level for a heading.
func levelTitle(level string) string {
	segments := strings.Split(level, ".")
	switch {
	case len(segments) < 4:
		return "Organisation " + segments[len(segments)-1]
	case segments[2] == address.Teams:
		return "Team " + segments[3]
	case segments[2] == address.Users:
		return "User " + segments[3]
	default:
		return "Project " + segments[3]
	}
}

// ClaudeCode emits the files for Claude Code.
func ClaudeCode(input Input, result resolve.Result) (Output, error) {
	var output Output
	levelOrder := []string{}
	levelRules := map[string][]resolve.Chosen{}
	var permissionSets []declaration.PermissionSet
	var hooks []hookEntry
	servers := map[string]any{}
	variables := map[string]bool{}
	for _, chosen := range result.Chosen {
		switch chosen.Kind {
		case address.Rules:
			content := firstFile(chosen.Files)
			rule, selectors, _ := declaration.ParseRuleDeclaration("", []byte(content))
			_, body, _, _ := declaration.SplitFrontmatter(content)
			if len(rule.Paths) > 0 {
				paths, _ := yaml.Marshal(map[string][]string{"paths": rule.Paths})
				output.Files = append(output.Files, File{Path: path.Join(".claude", "rules", chosen.ID+".md"), Address: chosen.Address, Content: "---\n" + string(paths) + "---\n" + ruleHeader(chosen, selectors) + strings.TrimSpace(body) + "\n"})
				continue
			}
			if _, seen := levelRules[chosen.Level]; !seen {
				levelOrder = append(levelOrder, chosen.Level)
			}
			levelRules[chosen.Level] = append(levelRules[chosen.Level], chosen)
		case address.Skills:
			if chosen.ID == ReinSkillID {
				output.Notes = append(output.Notes, fmt.Sprintf("the skill %s is rein's own, written by sync; %s is not written", ReinSkillID, chosen.Address))
				continue
			}
			for name, content := range chosen.Files {
				output.Files = append(output.Files, File{Path: path.Join(".claude", "skills", chosen.ID, name), Address: chosen.Address, Content: content})
			}
		case address.Hooks:
			var hook declaration.Hook
			err := yaml.Unmarshal([]byte(chosen.Files["hook.yaml"]), &hook)
			if err != nil {
				return Output{}, fmt.Errorf("%s: %w", chosen.Address, err)
			}
			for name, content := range chosen.Files {
				if name == "hook.yaml" {
					continue
				}
				output.Files = append(output.Files, File{Path: path.Join(".claude", "hooks", chosen.ID, name), Address: chosen.Address, Content: content})
			}
			hooks = append(hooks, hookEntry{event: hook.Event, matcher: hook.Matcher, command: "\"$CLAUDE_PROJECT_DIR\"/" + path.Join(".claude", "hooks", chosen.ID, hook.Run), timeout: hook.TimeoutSeconds})
		case address.Permissions:
			var set declaration.PermissionSet
			err := yaml.Unmarshal([]byte(firstFile(chosen.Files)), &set)
			if err != nil {
				return Output{}, fmt.Errorf("%s: %w", chosen.Address, err)
			}
			permissionSets = append(permissionSets, set)
		case address.MCPServers:
			var server declaration.ToolServer
			err := yaml.Unmarshal([]byte(firstFile(chosen.Files)), &server)
			if err != nil {
				return Output{}, fmt.Errorf("%s: %w", chosen.Address, err)
			}
			servers[chosen.ID] = mcpEntry(server)
			for _, value := range server.Env {
				for _, name := range declaration.References(value) {
					variables[name] = true
				}
			}
			for _, value := range server.Headers {
				for _, name := range declaration.References(value) {
					variables[name] = true
				}
			}
		}
	}
	// the level files and CLAUDE.md
	var imports []string
	for _, level := range levelOrder {
		name := levelFile(level, input.OrganizationSlug)
		var text strings.Builder
		fmt.Fprintf(&text, "# %s\n\n", levelTitle(level))
		rules := levelRules[level]
		sort.Slice(rules, func(left, right int) bool { return rules[left].ID < rules[right].ID })
		for _, chosen := range rules {
			content := firstFile(chosen.Files)
			_, selectors, _ := declaration.ParseRuleDeclaration("", []byte(content))
			_, body, _, _ := declaration.SplitFrontmatter(content)
			fmt.Fprintf(&text, "## %s\n\n%s%s\n\n", chosen.ID, ruleHeader(chosen, selectors), strings.TrimSpace(body))
		}
		filePath := path.Join(ReinFolder, name+".md")
		output.Files = append(output.Files, File{Path: filePath, Address: "generated", Content: strings.TrimRight(text.String(), "\n") + "\n"})
		imports = append(imports, "@"+filePath)
	}
	var claude strings.Builder
	fmt.Fprintf(&claude, "<!-- Generated by rein sync from the org repository of %s; do not edit, rein propose changes it. -->\n", input.OrganizationSlug)
	fmt.Fprintf(&claude, "# Agent settings of %s\n\n", input.ProjectName)
	fmt.Fprintf(&claude, "These instructions come from the organisation %s, the teams %s, the project %s and the user %s, each in its own file below. When two disagree, the higher level wins: organisation over team, team over project, project over user. A rule marked required may not be weakened by anything below it.\n\n", input.OrganizationSlug, strings.Join(input.Teams, ", "), input.ProjectSlug, orNone(input.UserSlug))
	if len(imports) == 0 {
		fmt.Fprintf(&claude, "No always-on instructions apply to this project yet.\n")
	}
	for _, line := range imports {
		fmt.Fprintf(&claude, "%s\n", line)
	}
	output.Files = append(output.Files, File{Path: "CLAUDE.md", Address: "generated", Content: claude.String()})
	// rein's own skill, so the agent knows how these files are managed
	output.Files = append(output.Files, File{Path: path.Join(".claude", "skills", ReinSkillID, "SKILL.md"), Address: "generated", Content: ReinSkill})
	// settings: hooks, permissions, the session-start hook
	settings, notes, err := mergeSettings(input, permissionSets, hooks)
	if err != nil {
		return Output{}, err
	}
	output.Notes = append(output.Notes, notes...)
	output.Files = append(output.Files, File{Path: SettingsFile, Address: "generated", Content: settings})
	// tool servers, rein's own among them
	if input.ReinExecutable != "" {
		servers[ReinServerID] = map[string]any{"command": input.ReinExecutable, "args": []string{"mcp"}}
	}
	if len(servers) > 0 {
		encoded, err := json.MarshalIndent(map[string]any{"mcpServers": servers}, "", "  ")
		if err != nil {
			return Output{}, err
		}
		output.Files = append(output.Files, File{Path: ".mcp.json", Address: "generated", Content: string(encoded) + "\n"})
	}
	for name := range variables {
		output.Variables = append(output.Variables, name)
	}
	sort.Strings(output.Variables)
	sort.Slice(output.Files, func(left, right int) bool { return output.Files[left].Path < output.Files[right].Path })
	return output, nil
}

// ruleHeader is the comment that names a rule's address and enforcement.
func ruleHeader(chosen resolve.Chosen, selectors declaration.Selectors) string {
	enforcement := selectors.Enforcement
	if enforcement == "" {
		enforcement = declaration.EnforcementDefault
	}
	return fmt.Sprintf("<!-- %s, %s -->\n", chosen.Address, enforcement)
}

func firstFile(files map[string]string) string {
	var names []string
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return ""
	}
	return files[names[0]]
}

func orNone(text string) string {
	if text == "" {
		return "none"
	}
	return text
}

// mcpEntry is a tool server as .mcp.json holds it.
func mcpEntry(server declaration.ToolServer) map[string]any {
	entry := map[string]any{}
	if server.Transport != "stdio" {
		entry["type"] = server.Transport
	}
	if server.Command != "" {
		entry["command"] = server.Command
	}
	if len(server.Args) > 0 {
		entry["args"] = server.Args
	}
	if server.URL != "" {
		entry["url"] = server.URL
	}
	if len(server.Env) > 0 {
		entry["env"] = server.Env
	}
	if len(server.Headers) > 0 {
		entry["headers"] = server.Headers
	}
	return entry
}

type hookEntry struct {
	event   string
	matcher string
	command string
	timeout int
}

// mergeSettings writes rein's hooks and permissions into the existing
// settings: everything else stays, the developer's own allow entries stay
// unless a deny covers them, and the session-start hook reruns sync.
func mergeSettings(input Input, sets []declaration.PermissionSet, hooks []hookEntry) (string, []string, error) {
	settings := map[string]any{}
	if strings.TrimSpace(input.ExistingSettings) != "" {
		err := json.Unmarshal([]byte(input.ExistingSettings), &settings)
		if err != nil {
			return "", nil, fmt.Errorf("%s is not valid JSON: %w", SettingsFile, err)
		}
	}
	var notes []string
	allow, deny, ask := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, set := range sets {
		for _, rule := range set.Allow {
			allow[rule] = true
		}
		for _, rule := range set.Deny {
			deny[rule] = true
		}
		for _, rule := range set.Ask {
			ask[rule] = true
		}
	}
	existingPermissions, _ := settings["permissions"].(map[string]any)
	if existingAllow, ok := existingPermissions["allow"].([]any); ok {
		for _, raw := range existingAllow {
			rule, isString := raw.(string)
			if !isString || allow[rule] {
				continue
			}
			covered := ""
			for denied := range deny {
				if declaration.CoversPermission(denied, rule) {
					covered = denied
				}
			}
			if covered != "" {
				notes = append(notes, fmt.Sprintf("your allow rule %q is dropped: the organisation denies %q", rule, covered))
				continue
			}
			allow[rule] = true
		}
	}
	permissions := map[string]any{}
	for key, value := range existingPermissions {
		if key != "allow" && key != "deny" && key != "ask" {
			permissions[key] = value
		}
	}
	if len(allow) > 0 {
		permissions["allow"] = sortedKeys(allow)
	}
	if len(deny) > 0 {
		permissions["deny"] = sortedKeys(deny)
	}
	if len(ask) > 0 {
		permissions["ask"] = sortedKeys(ask)
	}
	if len(permissions) > 0 {
		settings["permissions"] = permissions
	} else {
		delete(settings, "permissions")
	}
	hooksByEvent := map[string][]any{}
	sessionStart := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": fmt.Sprintf("%q sync --hook", input.ReinExecutable)}}}
	hooksByEvent["SessionStart"] = append(hooksByEvent["SessionStart"], sessionStart)
	sort.Slice(hooks, func(left, right int) bool { return hooks[left].command < hooks[right].command })
	for _, hook := range hooks {
		entry := map[string]any{"type": "command", "command": hook.command}
		if hook.timeout > 0 {
			entry["timeout"] = hook.timeout
		}
		group := map[string]any{"hooks": []any{entry}}
		if hook.matcher != "" {
			group["matcher"] = hook.matcher
		}
		hooksByEvent[hook.event] = append(hooksByEvent[hook.event], group)
	}
	hooksValue := map[string]any{}
	for event, groups := range hooksByEvent {
		hooksValue[event] = groups
	}
	settings["hooks"] = hooksValue
	encoded, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return "", nil, err
	}
	return string(encoded) + "\n", notes, nil
}

func sortedKeys(set map[string]bool) []string {
	var keys []string
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
