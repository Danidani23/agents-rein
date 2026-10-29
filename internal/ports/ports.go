// Package ports holds the interfaces the use cases need from the outside
// world. A port exists only where a second implementation is plausible: the
// host (GitHub, later GitLab), the repository (git), the agent (Claude Code,
// later the planned ones), the state store, and the prompter.
package ports

import (
	"context"
	"fmt"
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
	// RepositorySettingsURL is the page where a person administers
	// owner/name by hand, its deletion included; rein never deletes (D26).
	RepositorySettingsURL(owner string, name string) string
}

// Release is one published build of rein: its tag and its downloadable
// files by name.
type Release struct {
	Version string
	Assets  map[string]string
}

// Releases is where rein's own releases are published, GitHub Releases of
// this repository, for the update check and the self-update.
type Releases interface {
	// Latest is the newest release.
	Latest() (Release, error)
	// Download fetches one asset by its URL.
	Download(url string) ([]byte, error)
}

// Keychain keeps the tokens, one per login, in the operating system's own
// store; rein's files never hold one (D22, D25).
type Keychain interface {
	// Available says whether this machine has a keychain rein can use, or why not.
	Available() (reason string, ok bool)
	Store(token Token) error
	Load(login string) (token Token, found bool, err error)
	Delete(login string) error
	// StoreKey keeps a provider's key under the name of the variable that
	// would otherwise hold it, replacing an earlier one (Q59); LoadKey and
	// DeleteKey read and remove it, a missing one no error.
	StoreKey(name string, value string) error
	LoadKey(name string) (value string, found bool, err error)
	DeleteKey(name string) error
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
	// RemoteURL is the URL of the repository's origin, "" without one.
	RemoteURL(directory string) (string, error)
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
	// TrustsFolder says whether the agent has asked the developer to trust
	// the folder and been told yes, which gates what it reads there; false
	// when it never asked. An agent without such a dialog answers true.
	TrustsFolder(folder string) (bool, error)
	// TrustCommand is what the developer runs inside a folder to be asked
	// for that trust: the agent's own command.
	TrustCommand() string
}

// Judge is the model that reads prose for rein: one brief in, one answer
// out, and a reply into that conversation when the answer needs mending;
// rein keeps nothing between calls, a reply is handed the conversation it
// goes into (D23, P2.58). The organisation's provider decides which backend
// stands here; the agent's own CLI is the local fallback.
type Judge interface {
	// Name says which backend answers, for the report.
	Name() string
	// Available says whether the backend can answer on this machine, or why not.
	Available() (reason string, ok bool)
	// Ask sends the brief and returns the answer's text, with the id of
	// the conversation it ran as, "" when the backend keeps none, so a
	// person can open it again. progress, which may be nil, hears how far
	// the answer has come while it is written.
	Ask(brief string, model string, progress func(JudgeProgress)) (answer string, session string, err error)
	// Reply sends a message into a conversation, after its answer, and
	// returns the answer to it with the conversation's id: a backend that
	// keeps conversations goes on in the one it kept, one that keeps none
	// sends the brief and the answer again before the message.
	Reply(conversation JudgeConversation, message string, model string, progress func(JudgeProgress)) (answer string, session string, err error)
	// ResumeCommand is the command a person runs to open a conversation
	// again, "" when the backend keeps none.
	ResumeCommand(session string) string
}

// JudgeConversation is a brief, the answer it got, and the id of the
// conversation the backend kept, "" when it keeps none.
type JudgeConversation struct {
	Brief   string
	Answer  string
	Session string
}

// JudgeProgress is how far an answer has come: the model still thinking,
// or the text it has written so far.
type JudgeProgress struct {
	Thinking bool
	Written  string
}

// MachineLock keeps two commands from changing this machine at once: a
// command that writes holds it from its start to its end (P2.24), and the
// commands that want it take turns by the rules of D56.
type MachineLock interface {
	// Acquire takes the lock for holder once every command that asked
	// before it is done (R2), until ctx ends; waiting is told what it
	// waits for, each time that changes. A holder identical to one in line,
	// the same command in the same folder, never runs: Acquire waits for
	// that one and returns a Joined with how it ended (R3). release gives
	// the lock back with the holder's outcome, nil when it worked.
	Acquire(ctx context.Context, holder LockHolder, waiting func(LockWait)) (release func(outcome error), err error)
}

// LockHolder is a command that holds the machine lock or waits for it:
// what was typed, by which caller, in which folder, and since when. A
// holder in another process is known only by its process.
type LockHolder struct {
	Command string
	Caller  string
	Folder  string
	Since   time.Time
	Process int
}

// String names the holder as a waiting command says it.
func (holder LockHolder) String() string {
	since := holder.Since.Format("15:04:05")
	if holder.Command == "" {
		return fmt.Sprintf("another rein, process %d, since %s", holder.Process, since)
	}
	return fmt.Sprintf("%s in %s, run from %s since %s", holder.Command, holder.Folder, holder.Caller, since)
}

// LockWait is what a waiting command waits for: the holder, how many
// commands are ahead of it, and whether it waits for an identical one,
// which it joins instead of running twice.
type LockWait struct {
	Holder  LockHolder
	Ahead   int
	Joining bool
}

// Joined is the end of a command that ran not at all, because an
// identical one was in line: that one, and how it ended. It unwraps to the
// outcome, so the exit code is the one that ran.
type Joined struct {
	Holder  LockHolder
	Outcome error
}

func (joined Joined) Error() string {
	ended := "it worked"
	if joined.Outcome != nil {
		ended = "it ended: " + joined.Outcome.Error()
	}
	return fmt.Sprintf("the same command ran: %s; this one waited for it instead of running twice, and %s", joined.Holder, ended)
}

func (joined Joined) Unwrap() error {
	return joined.Outcome
}

// State persists the machine model.
type State interface {
	Load() (machine.State, error)
	Save(machine.State) error
}

// Documents keeps rein's own records on this machine, each a document
// under the name its file had: state.yaml, removals.yaml,
// inserts/<organisation>.yaml, snapshots/<name>.json. In use it is the
// encrypted store the background process opens (D51).
type Documents interface {
	// Read returns a document; a missing one is not found, no error.
	Read(name string) (content []byte, found bool, err error)
	// Write creates or replaces a document whole.
	Write(name string, content []byte) error
	// Remove deletes a document; a missing one is no error.
	Remove(name string) error
	// List names the documents whose name starts with prefix, sorted.
	List(prefix string) (names []string, err error)
	// Erase removes every document, the store itself and its key.
	Erase() error
	// Where names the store, as a plan or a message shows it.
	Where() string
}

// Text is a free-text question.
type Text struct {
	Title       string
	Description string
	Explanation string
	Default     string
	// Optional lets Enter answer with nothing when there is no default.
	Optional bool
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
