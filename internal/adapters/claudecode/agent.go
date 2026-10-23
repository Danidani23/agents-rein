package claudecode

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// Name is the agent's slug.
const Name = "claude-code"

// Agent is Claude Code as rein sees it: its command, its home folder and the
// placements made there. It is the Agent port.
type Agent struct{}

var _ ports.Agent = Agent{}

// Name returns claude-code.
func (Agent) Name() string {
	return Name
}

// Installed reports whether the claude command is on the PATH.
func (Agent) Installed() bool {
	_, err := exec.LookPath("claude")
	return err == nil
}

// Home is CLAUDE_CONFIG_DIR when set, else ~/.claude.
func (Agent) Home() (string, error) {
	return ClaudeHome()
}

// TrustsFolder reads Claude Code's own config, .claude.json, which records
// per project folder whether the developer accepted the trust dialog; the
// approvals in a local settings file apply only after that. The folder is
// looked up as given and with its links resolved. No file, no entry, or a
// file that does not parse all mean not trusted yet.
func (Agent) TrustsFolder(folder string) (bool, error) {
	configPath, err := claudeConfigFile()
	if err != nil {
		return false, err
	}
	content, err := os.ReadFile(configPath)
	if err != nil {
		return false, nil
	}
	var config struct {
		Projects map[string]struct {
			HasTrustDialogAccepted bool `json:"hasTrustDialogAccepted"`
		} `json:"projects"`
	}
	err = json.Unmarshal(content, &config)
	if err != nil {
		return false, nil
	}
	candidates := []string{filepath.Clean(folder)}
	if resolved, err := filepath.EvalSymlinks(folder); err == nil && resolved != candidates[0] {
		candidates = append(candidates, resolved)
	}
	for _, candidate := range candidates {
		if config.Projects[candidate].HasTrustDialogAccepted {
			return true, nil
		}
	}
	return false, nil
}

// claudeConfigFile is Claude Code's own config: .claude.json under
// CLAUDE_CONFIG_DIR when set, else in the user's home folder.
func claudeConfigFile() (string, error) {
	if value := os.Getenv("CLAUDE_CONFIG_DIR"); value != "" {
		return filepath.Join(value, ".claude.json"), nil
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(userHome, ".claude.json"), nil
}

// HarnessPaths are the settings under the home: the global instructions,
// the settings file with permissions and hooks, and the folders Claude Code
// reads rules, skills, agents and commands from. Transcripts, memory, todos
// and plugin caches are left out.
func (Agent) HarnessPaths() []string {
	return []string{"CLAUDE.md", "settings.json", "rules", "skills", "agents", "commands"}
}

// ProjectHarnessPaths are the settings Claude Code reads in a project that
// rein owns: the instructions, the .claude folder with rules, skills, agents
// and settings, and the tool servers. CLAUDE.local.md stays the developer's,
// where the memory shortcut writes, and is neither moved out nor generated.
func (Agent) ProjectHarnessPaths() []string {
	return []string{"CLAUDE.md", ".claude", ".mcp.json"}
}

// PersonalPaths is CLAUDE.local.md, where the memory shortcut writes.
func (Agent) PersonalPaths() []string {
	return []string{"CLAUDE.local.md"}
}

// Format is the harness format the compiler emits for Claude Code.
func (Agent) Format() string {
	return Name
}

// Unplace removes one placement when it is still what rein made: a link
// whose target is the recorded one, dangling or not, or a recorded copy.
func (Agent) Unplace(placed machine.Placement) (removed bool, reason string, err error) {
	info, err := os.Lstat(placed.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		existing, err := os.Readlink(placed.Path)
		if err != nil {
			return false, "", err
		}
		if existing != placed.Target {
			return false, "links to " + existing + ", not what rein recorded", nil
		}
		return true, "", os.Remove(placed.Path)
	}
	if placed.Mode != string(ModeCopy) {
		return false, "is a file rein did not place", nil
	}
	return true, "", os.RemoveAll(placed.Path)
}
