// Package distill turns a harness, the files an agent read in a project or
// a home as a snapshot captured them, into candidate declarations at one
// level, and compares them with the declarations an organisation already
// has. It is a library: pure functions, the comparison structural (P2.18 b).
// The model-judged pass over prose comes separately (P2.18 d).
package distill

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Danidani23/agents-rein/internal/app/2_services/address"
	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
)

// Candidate is one declaration distilled from a harness: its files relative
// to the level folder, where it came from, what the conversion noted, and
// after Compare its verdict against the organisation.
type Candidate struct {
	Kind    string   `yaml:"kind"`
	ID      string   `yaml:"id"`
	Sources []string `yaml:"sources"`
	Notes   []string `yaml:"notes,omitempty"`
	Verdict string   `yaml:"verdict,omitempty"`
	Related []string `yaml:"related,omitempty"`
	// Judgement is what the judge said, when it ran.
	Judgement *Judgement `yaml:"judgement,omitempty"`
	Files     map[string]string
}

// Verdicts of the structural comparison.
const (
	// VerdictNew means nothing in the organisation looks like it.
	VerdictNew = "new"
	// VerdictIdentical means a declaration with the same content exists.
	VerdictIdentical = "identical"
	// VerdictSameID means a declaration with the same id exists at another
	// level, which resolution would replace or refuse depending on enforcement.
	VerdictSameID = "same-id"
)

// ReportFile is the report's name inside a distillation folder.
const ReportFile = "report.yaml"

// Report is what a distillation found, written beside the candidates.
type Report struct {
	Version    int                `yaml:"version"`
	Level      string             `yaml:"level"`
	Snapshots  []string           `yaml:"snapshots"`
	Candidates []CandidateSummary `yaml:"candidates"`
	Skipped    []string           `yaml:"skipped,omitempty"`
	Judge      *JudgeRun          `yaml:"judge,omitempty"`
}

// CandidateSummary is a candidate without its files.
type CandidateSummary struct {
	Kind      string     `yaml:"kind"`
	ID        string     `yaml:"id"`
	Address   string     `yaml:"address"`
	Verdict   string     `yaml:"verdict"`
	Related   []string   `yaml:"related,omitempty"`
	Notes     []string   `yaml:"notes,omitempty"`
	Sources   []string   `yaml:"sources"`
	Judgement *Judgement `yaml:"judgement,omitempty"`
}

// Summarise lists the candidates for the report, addressed under level.
func Summarise(level string, candidates []Candidate) []CandidateSummary {
	var summaries []CandidateSummary
	for _, candidate := range candidates {
		summaries = append(summaries, CandidateSummary{Kind: candidate.Kind, ID: candidate.ID, Address: level + "." + candidate.Kind + "." + candidate.ID, Verdict: candidate.Verdict, Related: candidate.Related, Notes: candidate.Notes, Sources: candidate.Sources, Judgement: candidate.Judgement})
	}
	return summaries
}

// claudeSettings is the part of a Claude Code settings file distill reads.
type claudeSettings struct {
	Permissions struct {
		Allow []string `json:"allow"`
		Deny  []string `json:"deny"`
		Ask   []string `json:"ask"`
	} `json:"permissions"`
	Hooks map[string][]struct {
		Matcher string `json:"matcher"`
		Hooks   []struct {
			Type    string `json:"type"`
			Command string `json:"command"`
			Timeout int    `json:"timeout"`
		} `json:"hooks"`
	} `json:"hooks"`
}

// claudeMCP is a .mcp.json file.
type claudeMCP struct {
	Servers map[string]struct {
		Type    string            `json:"type"`
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		URL     string            `json:"url"`
		Env     map[string]string `json:"env"`
		Headers map[string]string `json:"headers"`
	} `json:"mcpServers"`
}

// ConvertClaudeCode turns the files of a Claude Code harness, keyed by path
// relative to the project folder or the home, into candidates: CLAUDE.md
// and the rules become rules, skills stay skills, the settings' permissions
// and hooks become a permission set and hooks, .mcp.json becomes tool
// servers. What it cannot place is listed as skipped.
func ConvertClaudeCode(files map[string]string) (candidates []Candidate, skipped []string, err error) {
	skills := map[string]map[string]string{}
	taken := map[string]bool{}
	var sorted []string
	for relative := range files {
		sorted = append(sorted, relative)
	}
	sort.Strings(sorted)
	for _, relative := range sorted {
		content := files[relative]
		switch {
		case relative == "CLAUDE.md":
			candidates = append(candidates, ruleSections("instructions", "", content, relative, "always on", taken)...)
		case relative == "CLAUDE.local.md":
			candidates = append(candidates, ruleSections("local-instructions", "local-", content, relative, "a personal file; consider proposing it at your user level instead", taken)...)
		case (strings.HasPrefix(relative, "rules/") || strings.HasPrefix(relative, ".claude/rules/")) && strings.HasSuffix(relative, ".md"):
			fileID := slugOf(strings.TrimSuffix(path.Base(relative), ".md"))
			candidates = append(candidates, ruleSections(fileID, fileID+"-", content, relative, "", taken)...)
		case strings.HasPrefix(relative, "skills/") || strings.HasPrefix(relative, ".claude/skills/"):
			inside := strings.TrimPrefix(strings.TrimPrefix(relative, ".claude/"), "skills/")
			name, rest, found := strings.Cut(inside, "/")
			if !found {
				skipped = append(skipped, relative+": not inside a skill folder")
				continue
			}
			if skills[name] == nil {
				skills[name] = map[string]string{}
			}
			skills[name][rest] = content
		case relative == ".claude/settings.json" || relative == "settings.json" || relative == ".claude/settings.local.json":
			suffix := ""
			if strings.HasSuffix(relative, "settings.local.json") {
				suffix = "-local"
			}
			converted, notes, err := convertSettings(relative, content, suffix)
			if err != nil {
				return nil, nil, err
			}
			candidates = append(candidates, converted...)
			skipped = append(skipped, notes...)
		case relative == ".mcp.json":
			converted, err := convertMCP(relative, content)
			if err != nil {
				return nil, nil, err
			}
			candidates = append(candidates, converted...)
		case strings.HasPrefix(relative, ".claude/agents/") || strings.HasPrefix(relative, "agents/"):
			skipped = append(skipped, relative+": agents are not distilled yet")
		default:
			skipped = append(skipped, relative+": not a harness file distill knows")
		}
	}
	var names []string
	for name := range skills {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		candidate := Candidate{Kind: address.Skills, ID: slugOf(name), Files: map[string]string{}}
		for rest, content := range skills[name] {
			candidate.Files[path.Join("skills", slugOf(name), rest)] = content
			candidate.Sources = append(candidate.Sources, path.Join(".claude/skills", name, rest))
		}
		sort.Strings(candidate.Sources)
		if candidate.ID != name {
			candidate.Notes = append(candidate.Notes, fmt.Sprintf("renamed from %q to a slug", name))
		}
		candidates = append(candidates, candidate)
	}
	return candidates, skipped, nil
}

// section is a part of a Markdown file: the text before its first ##
// heading, with an empty heading, or one heading with its text.
type section struct {
	heading string
	text    string
}

// sectionsOf cuts a Markdown file at its ## headings, outside code fences.
// The frontmatter is returned apart, for every part to carry, and each part
// is trimmed of its trailing blank lines.
func sectionsOf(content string) (frontmatter string, sections []section) {
	body := content
	if strings.HasPrefix(content, "---\n") {
		end := strings.Index(content[4:], "\n---\n")
		if end >= 0 {
			frontmatter = content[:4+end+5]
			body = content[4+end+5:]
		}
	}
	current := section{}
	inFence := false
	for _, line := range strings.SplitAfter(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
		}
		if !inFence && strings.HasPrefix(line, "## ") {
			sections = append(sections, current)
			current = section{heading: strings.TrimSpace(strings.TrimPrefix(line, "## "))}
		}
		current.text += line
	}
	sections = append(sections, current)
	for index := range sections {
		sections[index].text = strings.TrimRight(sections[index].text, " \t\n")
		if sections[index].text != "" {
			sections[index].text += "\n"
		}
	}
	return frontmatter, sections
}

// ruleSections turns a file of rules into one candidate per ## section
// (D34): the text before the first heading keeps the file's id, a section
// is named after its heading, before a dash or a colon. A file without
// sections stays one candidate, as it was. Ids are unique across the
// harness, a repeated one numbered.
func ruleSections(fileID string, sectionPrefix string, content string, source string, note string, taken map[string]bool) []Candidate {
	unique := func(id string) string {
		candidate := id
		for count := 2; taken[candidate]; count++ {
			candidate = fmt.Sprintf("%s-%d", id, count)
		}
		taken[candidate] = true
		return candidate
	}
	frontmatter, sections := sectionsOf(content)
	if len(sections) == 1 {
		return []Candidate{rule(unique(fileID), content, source, noteFor(source, "", note))}
	}
	var candidates []Candidate
	for _, part := range sections {
		if strings.TrimSpace(part.text) == "" {
			continue
		}
		id := fileID
		if part.heading != "" {
			name := part.heading
			for _, separator := range []string{" — ", " – ", ": "} {
				name, _, _ = strings.Cut(name, separator)
			}
			slug := slugOf(name)
			if slug == "" {
				slug = "section"
			}
			id = sectionPrefix + slug
		}
		candidates = append(candidates, rule(unique(id), frontmatter+part.text, source, noteFor(source, part.heading, note)))
	}
	return candidates
}

// noteFor says where a rule came from: the project's instructions, a
// section of a file, and what else the file's kind adds.
func noteFor(source string, heading string, note string) string {
	switch {
	case heading != "" && note != "":
		return fmt.Sprintf("the section %q of %s; %s", heading, source, note)
	case heading != "":
		return fmt.Sprintf("the section %q of %s", heading, source)
	case source == "CLAUDE.md":
		return "the project's instructions; " + note
	}
	return note
}

func rule(id string, content string, source string, note string) Candidate {
	candidate := Candidate{Kind: address.Rules, ID: id, Sources: []string{source}, Files: map[string]string{path.Join("rules", id+".md"): content}}
	if note != "" {
		candidate.Notes = append(candidate.Notes, note)
	}
	return candidate
}

func convertSettings(source string, content string, suffix string) (candidates []Candidate, skipped []string, err error) {
	var settings claudeSettings
	err = json.Unmarshal([]byte(content), &settings)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", source, err)
	}
	if len(settings.Permissions.Allow)+len(settings.Permissions.Deny)+len(settings.Permissions.Ask) > 0 {
		set := declaration.PermissionSet{Selectors: declaration.Selectors{Description: "permissions distilled from " + source}, Format: "claude-code", Allow: settings.Permissions.Allow, Deny: settings.Permissions.Deny, Ask: settings.Permissions.Ask}
		encoded, err := yaml.Marshal(set)
		if err != nil {
			return nil, nil, err
		}
		candidates = append(candidates, Candidate{Kind: address.Permissions, ID: "settings" + suffix, Sources: []string{source}, Files: map[string]string{path.Join("permissions", "settings"+suffix+".yaml"): string(encoded)}})
	}
	var events []string
	for event := range settings.Hooks {
		events = append(events, event)
	}
	sort.Strings(events)
	for _, event := range events {
		index := 0
		for _, group := range settings.Hooks[event] {
			for _, hook := range group.Hooks {
				if hook.Type != "command" && hook.Type != "" {
					skipped = append(skipped, fmt.Sprintf("%s: hook of type %q on %s is not distilled", source, hook.Type, event))
					continue
				}
				index++
				id := fmt.Sprintf("%s-%d%s", strings.ToLower(event), index, suffix)
				declared := declaration.Hook{Selectors: declaration.Selectors{Description: fmt.Sprintf("hook distilled from %s on %s", source, event)}, Format: "claude-code", Event: event, Matcher: group.Matcher, Run: "run.sh", TimeoutSeconds: hook.Timeout}
				encoded, err := yaml.Marshal(declared)
				if err != nil {
					return nil, nil, err
				}
				candidates = append(candidates, Candidate{
					Kind: address.Hooks, ID: id, Sources: []string{source},
					Notes: []string{"the hook's command became run.sh; it runs on every machine the declaration reaches, so review it"},
					Files: map[string]string{path.Join("hooks", id, "hook.yaml"): string(encoded), path.Join("hooks", id, "run.sh"): "#!/bin/sh\n" + hook.Command + "\n"},
				})
			}
		}
	}
	return candidates, skipped, nil
}

func convertMCP(source string, content string) ([]Candidate, error) {
	var file claudeMCP
	err := json.Unmarshal([]byte(content), &file)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	var names []string
	for name := range file.Servers {
		names = append(names, name)
	}
	sort.Strings(names)
	var candidates []Candidate
	for _, name := range names {
		entry := file.Servers[name]
		server := declaration.ToolServer{Selectors: declaration.Selectors{Description: "tool server " + name + " distilled from " + source}, Transport: entry.Type, Command: entry.Command, Args: entry.Args, URL: entry.URL}
		if server.Transport == "" {
			server.Transport = "stdio"
			if entry.URL != "" {
				server.Transport = "http"
			}
		}
		var notes []string
		server.Env, server.Variables, notes = withReferences(entry.Env, name, nil, notes)
		server.Headers, server.Variables, notes = withReferences(entry.Headers, name, server.Variables, notes)
		encoded, err := yaml.Marshal(server)
		if err != nil {
			return nil, err
		}
		id := slugOf(name)
		candidate := Candidate{Kind: address.MCPServers, ID: id, Sources: []string{source}, Notes: notes, Files: map[string]string{path.Join("mcpservers", id+".yaml"): string(encoded)}}
		if id != name {
			candidate.Notes = append(candidate.Notes, fmt.Sprintf("renamed from %q to a slug", name))
		}
		candidates = append(candidates, candidate)
	}
	return candidates, nil
}

// withReferences replaces a literal that looks like a secret by a REIN_
// reference (D22) and describes the variable.
func withReferences(values map[string]string, server string, variables map[string]string, notes []string) (map[string]string, map[string]string, []string) {
	if len(values) == 0 {
		return nil, variables, notes
	}
	replaced := map[string]string{}
	for key, value := range values {
		if len(declaration.References(value)) == 0 && declaration.LooksLikeSecret(value) {
			variable := declaration.VariablePrefix + strings.ToUpper(slugOf(server)+"_"+key)
			variable = strings.ReplaceAll(variable, "-", "_")
			if variables == nil {
				variables = map[string]string{}
			}
			variables[variable] = fmt.Sprintf("was a literal value for %s in the harness; set it in your shell, never in a declaration", key)
			replaced[key] = "${" + variable + "}"
			notes = append(notes, fmt.Sprintf("%s held a secret; it is now the reference ${%s}, which every developer sets in their shell", key, variable))
			continue
		}
		replaced[key] = value
	}
	return replaced, variables, notes
}

// slugOf lowercases a name and keeps letters, digits and single hyphens.
func slugOf(name string) string {
	var builder strings.Builder
	lastHyphen := true
	for _, character := range strings.ToLower(name) {
		switch {
		case (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9'):
			builder.WriteRune(character)
			lastHyphen = false
		case !lastHyphen:
			builder.WriteByte('-')
			lastHyphen = true
		}
	}
	return strings.TrimRight(builder.String(), "-")
}

// Identity is what makes two declarations the same thing: for a rule the
// body, for a skill its files, for a tool server its transport, command and
// arguments or its URL, for a permission set its rules, for a hook its
// event, matcher and script.
func Identity(kind string, files map[string]string) string {
	var parts []string
	var paths []string
	for relative := range files {
		paths = append(paths, relative)
	}
	sort.Strings(paths)
	for _, relative := range paths {
		content := files[relative]
		switch kind {
		case address.MCPServers:
			var server declaration.ToolServer
			_ = yaml.Unmarshal([]byte(content), &server)
			parts = append(parts, server.Transport, server.Command, strings.Join(server.Args, "\x00"), server.URL)
		case address.Permissions:
			var set declaration.PermissionSet
			_ = yaml.Unmarshal([]byte(content), &set)
			for _, list := range [][]string{set.Allow, set.Deny, set.Ask} {
				sortedList := append([]string(nil), list...)
				sort.Strings(sortedList)
				parts = append(parts, strings.Join(sortedList, "\x00"))
			}
		case address.Hooks:
			if path.Base(relative) == "hook.yaml" {
				var hook declaration.Hook
				_ = yaml.Unmarshal([]byte(content), &hook)
				parts = append(parts, hook.Event, hook.Matcher)
			} else {
				parts = append(parts, strings.TrimSpace(content))
			}
		default:
			parts = append(parts, strings.TrimSpace(content))
		}
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x01")))
	return hex.EncodeToString(sum[:8])
}
