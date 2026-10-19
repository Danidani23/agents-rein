// Package ports holds the interfaces the use cases need from the outside
// world. A port exists only where a second implementation is plausible: the
// host (GitHub, later GitLab), the repository (git), the agent (Claude Code,
// later the planned ones), the state store, and the prompter.
package ports

import "github.com/Danidani23/agents-rein/internal/app/2_services/machine"

// Identity is who rein acts for: the host account, read every time and never
// stored (D8, D9). Slug is the address segment derived from the login.
type Identity struct {
	Login string
	Slug  string
}

// NewRepository describes a repository to create on the host.
type NewRepository struct {
	Owner       string
	Name        string
	Description string
	Private     bool
}

// Host is the forge that holds org repositories and identities.
type Host interface {
	// Identity returns the active account, or an error that says how to log in.
	Identity() (Identity, error)
	// GitProtocol is what the user's host tooling clones and pushes with,
	// "ssh" or "https".
	GitProtocol() (string, error)
	// CreateRepository creates the repository and returns the remote URL to
	// push to and clone from.
	CreateRepository(repository NewRepository) (remote string, err error)
	// DeleteRepository deletes owner/name: the compensation of a failed create.
	DeleteRepository(owner string, name string) error
	// ProtectMainBranch requires a pull request with a code-owner review on
	// the default branch, administrators excepted (D13). A host that refuses
	// it for the account's plan returns the reason and no error.
	ProtectMainBranch(owner string, name string) (refusedReason string, err error)
}

// Repository is version control on this machine.
type Repository interface {
	// Preflight checks that version control is installed, before anything
	// changes.
	Preflight() error
	// Clone clones remote into directory, which must not exist, without ever
	// prompting.
	Clone(remote string, directory string) error
	// Publish makes directory a repository on main with one commit of
	// everything in it, and pushes it to remote as origin.
	Publish(directory string, remote string, message string) error
	// Pull brings a clone up to date with its origin, fast-forward only.
	Pull(directory string) error
	// CommitPaths commits the given paths, relative to directory, as one commit.
	CommitPaths(directory string, relativePaths []string, message string) error
	// Push pushes the clone's main to its origin.
	Push(directory string) error
	// ResetToRemote discards every local change and commit, back to origin's main.
	ResetToRemote(directory string) error
}

// Agent is one coding agent rein writes settings for.
type Agent interface {
	// Name is the agent's slug, such as claude-code.
	Name() string
	// Installed reports whether the agent's command is on this machine.
	Installed() bool
	// Home is the folder the agent reads its user-level settings from.
	Home() (string, error)
	// HarnessPaths are the files and folders under Home that hold settings:
	// instructions, rules, skills, permissions; never transcripts or caches.
	HarnessPaths() []string
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

// Select is a question answered by picking one choice, or several through
// ChooseMany, where Default may name several separated by commas and
// Optional allows none.
type Select struct {
	Title       string
	Description string
	Explanation string
	Choices     []Choice
	Default     string
	Optional    bool
}

// Prompter asks the questions of one command.
type Prompter interface {
	CanAsk() bool
	Intro(text string)
	Ask(question Text) (string, error)
	Choose(question Select) (string, error)
	ChooseMany(question Select) ([]string, error)
	Confirm(question string, explanation string) (bool, error)
}
