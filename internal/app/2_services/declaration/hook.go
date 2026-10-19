package declaration

import (
	"fmt"
	"strings"
)

// Hook is a hook declaration, hooks/<id>/hook.yaml, with its script beside
// it in the same folder. A hook runs on every developer's machine, so its
// script lives inside the declaration and nowhere else: sync places the
// folder into the project clone, never into the agent's home (D21), and
// the settings it writes point at the placed script.
type Hook struct {
	Selectors      `yaml:",inline"`
	Format         string `yaml:"format"`
	Event          string `yaml:"event"`
	Matcher        string `yaml:"matcher,omitempty"`
	Run            string `yaml:"run"`
	TimeoutSeconds int    `yaml:"timeout_seconds,omitempty"`
}

// ClaudeCodeHookEvents are the events Claude Code fires hooks on.
var ClaudeCodeHookEvents = []string{"PreToolUse", "PostToolUse", "Notification", "UserPromptSubmit", "Stop", "SubagentStop", "PreCompact", "SessionStart", "SessionEnd"}

// ParseHook reads one hook declaration; files are the names of the files in
// the hook's folder, which must hold the script run names.
func ParseHook(content []byte, files []string) (hook Hook, problems []Problem) {
	problems = decodeStrict(content, &hook, "hook.yaml")
	if problems != nil {
		return Hook{}, problems
	}
	problems = append(problems, hook.Selectors.check()...)
	problems = append(problems, checkFormat(hook.Format)...)
	if hook.Format == "claude-code" {
		known := false
		for _, event := range ClaudeCodeHookEvents {
			if hook.Event == event {
				known = true
			}
		}
		if !known {
			problems = append(problems, Problem{Severity: SeverityError, Message: fmt.Sprintf("event %q is not one Claude Code fires: %s", hook.Event, strings.Join(ClaudeCodeHookEvents, ", "))})
		}
	}
	switch {
	case hook.Run == "":
		problems = append(problems, Problem{Severity: SeverityError, Message: "run is required: the script in the hook's folder"})
	case strings.ContainsAny(hook.Run, "/\\") || hook.Run == "." || hook.Run == "..":
		problems = append(problems, Problem{Severity: SeverityError, Message: fmt.Sprintf("run %q must be a file in the hook's own folder, not a path", hook.Run)})
	default:
		present := false
		for _, file := range files {
			if file == hook.Run {
				present = true
			}
		}
		if !present {
			problems = append(problems, Problem{Severity: SeverityError, Message: fmt.Sprintf("run names %q, which is not in the hook's folder", hook.Run)})
		}
	}
	if hook.TimeoutSeconds < 0 {
		problems = append(problems, Problem{Severity: SeverityError, Message: "timeout_seconds cannot be negative"})
	}
	return hook, problems
}
