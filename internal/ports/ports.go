// Package ports holds the interfaces the use cases need from the outside
// world. A port exists only where a second implementation is plausible: the
// host (GitHub, later GitLab), the repository (git), the agent (Claude Code,
// later the planned ones), the state store, and the prompter.
package ports

import "github.com/Danidani23/agents-rein/internal/domain/machine"

// Identity is who rein acts for: the host account, read every time and never
// stored (D8, D9). Slug is the address segment derived from the login.
type Identity struct {
	Login string
	Slug  string
}

// Host is the forge that holds org repositories and identities.
type Host interface {
	// Identity returns the active account, or an error that says how to log in.
	Identity() (Identity, error)
}

// Repository is version control on this machine.
type Repository interface {
	// Clone clones remote into directory, which must not exist, without ever
	// prompting.
	Clone(remote string, directory string) error
}

// Agent is one coding agent rein writes settings for.
type Agent interface {
	// Name is the agent's slug, such as claude-code.
	Name() string
	// Installed reports whether the agent's command is on this machine.
	Installed() bool
	// Unplace removes one placement rein made, when it is still what rein
	// made; removed is false with an empty reason when nothing is there.
	Unplace(placed machine.Placement) (removed bool, reason string, err error)
}

// State persists the machine model.
type State interface {
	Load() (machine.State, error)
	Save(machine.State) error
}

// Text is a free-text question.
type Text struct {
	Title       string
	Description string
	Explanation string
	Default     string
	// Validate returns the reason an answer is refused, or "".
	Validate func(answer string) string
}

// Choice is one option of a selector.
type Choice struct {
	Name        string
	Description string
	// Unavailable is the reason the choice cannot be picked, or "".
	Unavailable string
}

// Select is a question answered by picking one choice.
type Select struct {
	Title       string
	Description string
	Explanation string
	Choices     []Choice
	Default     string
}

// Prompter asks the questions of one command.
type Prompter interface {
	CanAsk() bool
	Intro(text string)
	Ask(question Text) (string, error)
	Choose(question Select) (string, error)
	Confirm(question string, explanation string) (bool, error)
}
