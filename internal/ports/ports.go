// Package ports holds the interfaces the use cases need from the outside
// world. A port exists only where a second implementation is plausible: the
// host (GitHub, later GitLab), the repository (git), the agent (Claude Code,
// later the planned ones), the state store, and the prompter.
package ports

import (
	"time"

	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
)

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

// RepositoryInfo is what the host says about owner/name.
type RepositoryInfo struct {
	Exists   bool
	Empty    bool
	CloneURL string
}

// PullRequest describes a pull request to open from a pushed branch.
type PullRequest struct {
	Branch string
	Title  string
	Body   string
}

// Token is what the host handed rein for one login through the app's
// device flow (D25): it acts as that user and expires, with a refresh
// token to renew it. It lives in the keychain only.
type Token struct {
	Login            string    `json:"login"`
	AccessToken      string    `json:"access_token"`
	RefreshToken     string    `json:"refresh_token,omitempty"`
	ExpiresAt        time.Time `json:"expires_at,omitempty"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at,omitempty"`
}

// Expired says whether the access token is past use, with a minute to spare.
func (token Token) Expired(now time.Time) bool {
	return !token.ExpiresAt.IsZero() && token.ExpiresAt.Before(now.Add(time.Minute))
}

// DeviceCode is the first step of the device flow: the code the user types
// at the verification address.
type DeviceCode struct {
	DeviceCode      string
	UserCode        string
	VerificationURI string
	Interval        time.Duration
	ExpiresAt       time.Time
}

// RemoteAccess says which login's token a git operation on a remote uses.
type RemoteAccess struct {
	Login string
}

// Host is the forge that holds org repositories and identities, reached
// through its API with a token of the app (D25). Rein never deletes a
// repository (D26).
type Host interface {
	// Name is the host's slug, github.
	Name() string
	// StartDeviceFlow asks the host for a code the user approves.
	StartDeviceFlow() (DeviceCode, error)
	// PollDeviceFlow asks once whether the user approved; pending while not.
	PollDeviceFlow(code DeviceCode) (token Token, pending bool, err error)
	// RefreshToken renews an expired token.
	RefreshToken(token Token) (Token, error)
	// Identity is the user the token acts as.
	Identity(accessToken string) (Identity, error)
	// Owners lists where the user may create repositories: the login and
	// the organisations it belongs to that the app reaches.
	Owners(accessToken string) ([]string, error)
	// Repository describes owner/name.
	Repository(accessToken string, owner string, name string) (RepositoryInfo, error)
	// CreateRepository creates the repository and returns its clone URL.
	CreateRepository(accessToken string, repository NewRepository) (cloneURL string, err error)
	// ProtectMainBranch requires a pull request with a code-owner review on
	// the default branch, administrators excepted (D13). A host that refuses
	// it for the account's plan returns the reason and no error.
	ProtectMainBranch(accessToken string, owner string, name string) (refusedReason string, err error)
	// CreatePullRequest opens a pull request for a pushed branch.
	CreatePullRequest(accessToken string, owner string, name string, request PullRequest) (url string, err error)
	// InstallURL is where a user installs the app on an account.
	InstallURL() string
}

// Keychain keeps the tokens, one per login, in the operating system's own
// store; rein's files never hold one (D22, D25).
type Keychain interface {
	// Available says whether this machine has a keychain rein can use, or why not.
	Available() (reason string, ok bool)
	Store(token Token) error
	Load(login string) (token Token, found bool, err error)
	Delete(login string) error
}

// Repository is version control on this machine. Operations on a remote
// take the login whose token authenticates them.
type Repository interface {
	// Preflight checks that version control is installed, before anything
	// changes.
	Preflight() error
	// Clone clones remote into directory, which must not exist, without ever
	// prompting.
	Clone(access RemoteAccess, remote string, directory string) error
	// Publish makes directory a repository on main with one commit of
	// everything in it, and pushes it to remote as origin.
	Publish(access RemoteAccess, directory string, remote string, message string) error
	// Pull brings a clone up to date with its origin, fast-forward only.
	Pull(access RemoteAccess, directory string) error
	// CommitPaths commits the given paths, relative to directory, as one commit.
	CommitPaths(directory string, relativePaths []string, message string) error
	// Push pushes the clone's main to its origin.
	Push(access RemoteAccess, directory string) error
	// ResetToRemote discards every local change and commit, back to origin's main.
	ResetToRemote(directory string) error
	// Root is the top folder of the repository that holds directory.
	Root(directory string) (string, error)
	// RootCommits are the hashes of the commits without parents: the
	// fingerprint of a repository, which survives renames and forks.
	RootCommits(directory string) ([]string, error)
	// TrackedPaths are the paths among relativePaths that the repository
	// tracks, the files under a folder included.
	TrackedPaths(directory string, relativePaths []string) ([]string, error)
	// StagePaths stages the paths: additions, changes and deletions alike.
	StagePaths(directory string, relativePaths []string) error
	// HasStagedChanges reports whether a commit would have content.
	HasStagedChanges(directory string) (bool, error)
	// CommitStaged commits what is staged and returns the commit's hash.
	CommitStaged(directory string, message string) (hash string, err error)
	// CreateBranch makes a branch from the current one and switches to it.
	CreateBranch(directory string, branch string) error
	// Checkout switches to a branch.
	Checkout(directory string, branch string) error
	// DeleteBranch deletes a local branch, merged or not.
	DeleteBranch(directory string, branch string) error
	// PushBranch pushes a branch to origin.
	PushBranch(access RemoteAccess, directory string, branch string) error
	// DeleteRemoteBranch deletes a branch on origin.
	DeleteRemoteBranch(access RemoteAccess, directory string, branch string) error
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
	// ProjectHarnessPaths are the files and folders of a project clone the
	// agent reads its settings from, which init moves out and sync writes.
	ProjectHarnessPaths() []string
	// Format names the harness format the agent reads, which picks the
	// compiler; empty means rein has no compiler for it yet.
	Format() string
	// PersonalPaths are the files the agent writes for the developer in a
	// project clone, which rein never touches but keeps out of git.
	PersonalPaths() []string
	// Unplace removes one placement rein made, when it is still what rein
	// made; removed is false with an empty reason when nothing is there.
	Unplace(placed machine.Placement) (removed bool, reason string, err error)
}

// Judge is the model that reads prose for rein: one brief in, one answer
// out, no memory between calls (D23). The organisation's provider decides
// which backend stands here; the agent's own CLI is the local fallback.
type Judge interface {
	// Name says which backend answers, for the report.
	Name() string
	// Available says whether the backend can answer on this machine, or why not.
	Available() (reason string, ok bool)
	// Ask sends the brief and returns the answer's text.
	Ask(brief string) (answer string, err error)
}

// MachineLock keeps two rein processes from changing this machine at once:
// a command that writes holds it from its start to its end (P2.24).
type MachineLock interface {
	// Acquire takes the lock, waiting up to wait for another rein to finish,
	// and returns what releases it; a lock that cannot be had names its holder.
	Acquire(wait time.Duration) (release func(), err error)
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

// Decision is the answer to a confirmation that may also go back.
type Decision string

// The decisions.
const (
	DecisionYes  Decision = "yes"
	DecisionNo   Decision = "no"
	DecisionEdit Decision = "edit"
)

// Prompter asks the questions of one command.
type Prompter interface {
	CanAsk() bool
	Intro(text string)
	Ask(question Text) (string, error)
	Choose(question Select) (string, error)
	ChooseMany(question Select) ([]string, error)
	Confirm(question string, explanation string) (bool, error)
	// ConfirmOrEdit is Confirm with a third answer, e: ask everything again
	// with the previous answers as the defaults.
	ConfirmOrEdit(question string, explanation string) (Decision, error)
}
