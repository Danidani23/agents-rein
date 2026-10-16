package claudecode

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"

	"github.com/Danidani23/agents-rein/internal/domain/machine"
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
