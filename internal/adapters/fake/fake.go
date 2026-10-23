// Package fake holds in-memory adapters for every port, for the tests of the
// services, the orchestration and the gateway. A fake is an adapter too: it
// implements a port, only without the machine behind it.
package fake

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// Host is a version-control host reached with tokens, as the app's device
// flow hands them out. Login is the account every token acts as; a device
// flow approves after Pending polls. It records what it was asked to do.
type Host struct {
	Login             string
	Pending           int
	Organizations     []string
	Repositories      map[string]ports.RepositoryInfo
	Created           []ports.NewRepository
	FailCreate        bool
	ProtectionRefusal string
	FailProtection    bool
	ProtectedBranches []string
	PullRequests      []ports.PullRequest
	FailPullRequest   bool
	Refreshed         int
	polls             int
}

// Name is github, which the fake stands in for.
func (*Host) Name() string {
	return "github"
}

// InstallURL is a fake address.
func (*Host) InstallURL() string {
	return "https://github.com/apps/fake/installations/new"
}

// RepositorySettingsURL is a fake address.
func (*Host) RepositorySettingsURL(owner string, name string) string {
	return "https://github.com/" + owner + "/" + name + "/settings"
}

// StartDeviceFlow hands out a fixed code.
func (host *Host) StartDeviceFlow() (ports.DeviceCode, error) {
	host.polls = 0
	return ports.DeviceCode{DeviceCode: "device-1", UserCode: "ABCD-1234", VerificationURI: "https://github.com/login/device", Interval: time.Millisecond, ExpiresAt: time.Now().Add(time.Hour)}, nil
}

// PollDeviceFlow is pending for Pending polls, then approved.
func (host *Host) PollDeviceFlow(code ports.DeviceCode) (ports.Token, bool, error) {
	if host.Login == "" {
		return ports.Token{}, false, errors.New("the authorisation was declined on GitHub")
	}
	host.polls++
	if host.polls <= host.Pending {
		return ports.Token{}, true, nil
	}
	return ports.Token{AccessToken: "token-of-" + host.Login, RefreshToken: "refresh-of-" + host.Login, ExpiresAt: time.Now().Add(8 * time.Hour)}, false, nil
}

// RefreshToken renews with a fresh expiry.
func (host *Host) RefreshToken(token ports.Token) (ports.Token, error) {
	host.Refreshed++
	token.AccessToken = "renewed-" + token.AccessToken
	token.ExpiresAt = time.Now().Add(8 * time.Hour)
	return token, nil
}

// Identity is the account a token of this host acts as.
func (host *Host) Identity(accessToken string) (ports.Identity, error) {
	if accessToken == "" {
		return ports.Identity{}, errors.New("GitHub answered 401: Bad credentials")
	}
	login := strings.TrimPrefix(strings.TrimPrefix(accessToken, "renewed-"), "token-of-")
	return ports.Identity{Login: login, Slug: strings.ToLower(login)}, nil
}

// Owners is the login and the organisations.
func (host *Host) Owners(accessToken string) ([]string, error) {
	identity, err := host.Identity(accessToken)
	if err != nil {
		return nil, err
	}
	return append([]string{identity.Login}, host.Organizations...), nil
}

// Repository answers from Repositories, else "does not exist".
func (host *Host) Repository(accessToken string, owner string, name string) (ports.RepositoryInfo, error) {
	return host.Repositories[owner+"/"+name], nil
}

// CreateRepository records the request and returns a fake clone URL.
func (host *Host) CreateRepository(accessToken string, repository ports.NewRepository) (string, error) {
	if host.FailCreate {
		return "", errors.New("GitHub answered 422: name already exists on this account")
	}
	host.Created = append(host.Created, repository)
	cloneURL := "https://github.com/" + repository.Owner + "/" + repository.Name + ".git"
	if host.Repositories == nil {
		host.Repositories = map[string]ports.RepositoryInfo{}
	}
	host.Repositories[repository.Owner+"/"+repository.Name] = ports.RepositoryInfo{Exists: true, Empty: true, CloneURL: cloneURL}
	return cloneURL, nil
}

// ProtectMainBranch records the protection, or refuses or fails on demand.
func (host *Host) ProtectMainBranch(accessToken string, owner string, name string) (string, error) {
	if host.FailProtection {
		return "", errors.New("GitHub could not be reached: network down")
	}
	if host.ProtectionRefusal != "" {
		return host.ProtectionRefusal, nil
	}
	host.ProtectedBranches = append(host.ProtectedBranches, owner+"/"+name+"/main")
	return "", nil
}

// CreatePullRequest records the request and returns a fake URL.
func (host *Host) CreatePullRequest(accessToken string, owner string, name string, request ports.PullRequest) (string, error) {
	if host.FailPullRequest {
		return "", errors.New("GitHub answered 422: a pull request for this branch already exists")
	}
	host.PullRequests = append(host.PullRequests, request)
	return fmt.Sprintf("https://github.com/%s/%s/pull/%d", owner, name, len(host.PullRequests)), nil
}

// Keychain keeps tokens in memory.
type Keychain struct {
	Tokens      map[string]ports.Token
	Unavailable string
}

// Available is false with a reason when Unavailable is set.
func (keychain *Keychain) Available() (string, bool) {
	if keychain.Unavailable != "" {
		return keychain.Unavailable, false
	}
	return "", true
}

// Store keeps the token.
func (keychain *Keychain) Store(token ports.Token) error {
	if keychain.Tokens == nil {
		keychain.Tokens = map[string]ports.Token{}
	}
	keychain.Tokens[token.Login] = token
	return nil
}

// Load reads a token.
func (keychain *Keychain) Load(login string) (ports.Token, bool, error) {
	token, found := keychain.Tokens[login]
	return token, found, nil
}

// Delete removes a token.
func (keychain *Keychain) Delete(login string) error {
	delete(keychain.Tokens, login)
	return nil
}

// Repository "clones" by writing an org repository with the organisation
// record it was given under declarations/, unless the
// remote was published through it, in which case it copies what was
// published. An empty Record writes the folder without a record, Fail fails
// the clone, FailPublish fails the publish.
type Repository struct {
	Record         string
	Slug           string
	Fail           bool
	FailPublish    bool
	FailPush       bool
	FailCommit     bool
	FailPushBranch bool
	Published      map[string]string
	Commits        []string
	Staged         []string
	Branches       []string
	Current        string
	Deleted        []string
	RemoteDeleted  []string
}

// originMarker is the file in a fake clone that names its remote, as
// .git/config does in a real one, so a moved clone keeps its origin.
const originMarker = ".fake-origin"

// gitMarker makes a folder a fake project repository; it lists the root
// commits, one per line. trackedMarker lists the tracked paths.
const (
	gitMarker     = ".fake-git"
	trackedMarker = ".fake-tracked"
)

// NewProjectClone makes folder a fake repository with the given root commits
// and tracked paths.
func NewProjectClone(folder string, rootCommits []string, trackedPaths []string) error {
	err := os.MkdirAll(folder, 0o755)
	if err != nil {
		return err
	}
	err = os.WriteFile(filepath.Join(folder, gitMarker), []byte(strings.Join(rootCommits, "\n")), 0o644)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(folder, trackedMarker), []byte(strings.Join(trackedPaths, "\n")), 0o644)
}

// Preflight always passes: the fake needs no git.
func (*Repository) Preflight() error {
	return nil
}

// Clone writes the fake org repository into directory.
func (repository *Repository) Clone(access ports.RemoteAccess, remote string, directory string) error {
	if repository.Fail {
		return errors.New("git clone failed: repository not found")
	}
	published, found := repository.Published[remote]
	if found {
		err := copyTree(published, directory)
		if err != nil {
			return err
		}
		return rememberOrigin(directory, remote)
	}
	folder := filepath.Join(directory, "declarations")
	err := os.MkdirAll(folder, 0o755)
	if err != nil {
		return err
	}
	err = rememberOrigin(directory, remote)
	if err != nil {
		return err
	}
	if repository.Record == "" {
		return nil
	}
	return os.WriteFile(filepath.Join(folder, "organization.yaml"), []byte(repository.Record), 0o644)
}

// Publish remembers directory as the content of remote, so a later clone of
// the remote returns it.
func (repository *Repository) Publish(access ports.RemoteAccess, directory string, remote string, message string) error {
	if repository.FailPublish {
		return errors.New("git push failed: remote rejected")
	}
	err := rememberOrigin(directory, remote)
	if err != nil {
		return err
	}
	repository.Commits = append(repository.Commits, message)
	return repository.publish(directory, remote)
}

// Pull is a no-op: the fake remote never moves on its own.
func (*Repository) Pull(access ports.RemoteAccess, directory string) error {
	return nil
}

// CommitPaths records the message; the content counts at the push.
func (repository *Repository) CommitPaths(directory string, relativePaths []string, message string) error {
	repository.Commits = append(repository.Commits, message)
	return nil
}

// Push publishes the clone's current content under its origin.
func (repository *Repository) Push(access ports.RemoteAccess, directory string) error {
	if repository.FailPush {
		return errors.New("git push failed: remote rejected (protected branch)")
	}
	return repository.publish(directory, originOf(directory))
}

// ResetToRemote restores the clone from what its origin holds.
func (repository *Repository) ResetToRemote(directory string) error {
	remote := originOf(directory)
	published, found := repository.Published[remote]
	if !found {
		return nil
	}
	err := os.RemoveAll(directory)
	if err != nil {
		return err
	}
	err = copyTree(published, directory)
	if err != nil {
		return err
	}
	return rememberOrigin(directory, remote)
}

// Root walks up from directory to the folder with the git marker.
// RemoteURL is the origin a clone or a publish remembered, "" otherwise.
func (*Repository) RemoteURL(directory string) (string, error) {
	content, err := os.ReadFile(filepath.Join(directory, originMarker))
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(string(content)), nil
}

func (*Repository) Root(directory string) (string, error) {
	for folder := directory; ; folder = filepath.Dir(folder) {
		_, err := os.Stat(filepath.Join(folder, gitMarker))
		if err == nil {
			return folder, nil
		}
		if filepath.Dir(folder) == folder {
			return "", errors.New("fatal: not a git repository")
		}
	}
}

// RootCommits reads the git marker.
func (repository *Repository) RootCommits(directory string) ([]string, error) {
	root, err := repository.Root(directory)
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(filepath.Join(root, gitMarker))
	if err != nil {
		return nil, err
	}
	commits := strings.Fields(string(content))
	if len(commits) == 0 {
		return nil, errors.New("fatal: your current branch 'main' does not have any commits yet")
	}
	return commits, nil
}

// TrackedPaths reads the tracked marker and keeps what lies under the paths.
func (repository *Repository) TrackedPaths(directory string, relativePaths []string) ([]string, error) {
	root, err := repository.Root(directory)
	if err != nil {
		return nil, err
	}
	content, _ := os.ReadFile(filepath.Join(root, trackedMarker))
	var tracked []string
	for _, candidate := range strings.Fields(string(content)) {
		for _, relativePath := range relativePaths {
			if candidate == relativePath || strings.HasPrefix(candidate, strings.TrimSuffix(relativePath, "/")+"/") {
				tracked = append(tracked, candidate)
			}
		}
	}
	return tracked, nil
}

// StagePaths records what was staged.
func (repository *Repository) StagePaths(directory string, relativePaths []string) error {
	repository.Staged = append(repository.Staged, relativePaths...)
	return nil
}

// HasStagedChanges is true when something was staged.
func (repository *Repository) HasStagedChanges(directory string) (bool, error) {
	return len(repository.Staged) > 0, nil
}

// CommitStaged records the commit and returns a fixed hash.
func (repository *Repository) CommitStaged(directory string, message string) (string, error) {
	if repository.FailCommit {
		return "", errors.New("git commit failed: gpg failed to sign the data")
	}
	repository.Commits = append(repository.Commits, message)
	repository.Staged = nil
	return "f4ce0000f4ce0000f4ce0000f4ce0000f4ce0000", nil
}

// CreateBranch records the branch and makes it current.
func (repository *Repository) CreateBranch(directory string, branch string) error {
	repository.Branches = append(repository.Branches, branch)
	repository.Current = branch
	return nil
}

// Checkout makes the branch current; back on main the working tree is what
// the remote's main holds, as git drops the branch's files.
func (repository *Repository) Checkout(directory string, branch string) error {
	repository.Current = branch
	if branch == "main" {
		return repository.ResetToRemote(directory)
	}
	return nil
}

// DeleteBranch records the deletion.
func (repository *Repository) DeleteBranch(directory string, branch string) error {
	repository.Deleted = append(repository.Deleted, branch)
	return nil
}

// PushBranch publishes the clone's content under origin#branch.
func (repository *Repository) PushBranch(access ports.RemoteAccess, directory string, branch string) error {
	if repository.FailPushBranch {
		return errors.New("git push failed: could not read from remote")
	}
	return repository.publish(directory, originOf(directory)+"#"+branch)
}

// DeleteRemoteBranch records the deletion on the remote.
func (repository *Repository) DeleteRemoteBranch(access ports.RemoteAccess, directory string, branch string) error {
	repository.RemoteDeleted = append(repository.RemoteDeleted, branch)
	delete(repository.Published, originOf(directory)+"#"+branch)
	return nil
}

func rememberOrigin(directory string, remote string) error {
	err := os.MkdirAll(directory, 0o755)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(directory, originMarker), []byte(remote), 0o644)
}

func originOf(directory string) string {
	remote, err := os.ReadFile(filepath.Join(directory, originMarker))
	if err != nil {
		return ""
	}
	return string(remote)
}

func (repository *Repository) publish(directory string, remote string) error {
	if repository.Published == nil {
		repository.Published = map[string]string{}
	}
	copied, err := os.MkdirTemp("", "fake-remote-*")
	if err != nil {
		return err
	}
	err = copyTree(directory, copied)
	if err != nil {
		return err
	}
	repository.Published[remote] = copied
	return nil
}

// copyTree copies every regular file under source into destination.
func copyTree(source string, destination string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, content, 0o644)
	})
}

// Agent is an agent that records what it was asked to unplace, with a home
// folder the tests fill. A placement whose path contains "foreign" is
// reported as not rein's and kept.
type Agent struct {
	Present      bool
	HomePath     string
	HarnessFiles []string
	Unplaced     []string
	// HarnessFormat is what Format returns; "claude-code" makes sync compile
	// for the fake as for Claude Code.
	HarnessFormat string
	// Untrusted makes TrustsFolder answer false for every folder.
	Untrusted bool
}

// TrustsFolder is true unless the test set Untrusted.
func (agent *Agent) TrustsFolder(string) (bool, error) {
	return !agent.Untrusted, nil
}

// TrustCommand is the fake's own command.
func (*Agent) TrustCommand() string {
	return "fake-agent"
}

// Name is the agent's name.
func (*Agent) Name() string {
	return "fake-agent"
}

// Installed says whether the agent counts as installed.
func (agent *Agent) Installed() bool {
	return agent.Present
}

// Home is the test's folder for the agent.
func (agent *Agent) Home() (string, error) {
	if agent.HomePath == "" {
		return "", errors.New("fake agent has no home")
	}
	return agent.HomePath, nil
}

// HarnessPaths are the test's harness files, or the instructions file.
func (agent *Agent) HarnessPaths() []string {
	if len(agent.HarnessFiles) == 0 {
		return []string{"INSTRUCTIONS.md", "skills"}
	}
	return agent.HarnessFiles
}

// ProjectHarnessPaths are what the fake agent reads in a project; with the
// Claude Code format they are Claude Code's.
func (agent *Agent) ProjectHarnessPaths() []string {
	if agent.HarnessFormat == "claude-code" {
		return []string{"CLAUDE.md", ".claude", ".mcp.json"}
	}
	return []string{"INSTRUCTIONS.md", ".fake-agent"}
}

// Format is the fake's harness format.
func (agent *Agent) Format() string {
	return agent.HarnessFormat
}

// PersonalPaths is CLAUDE.local.md for the Claude Code format, else none.
func (agent *Agent) PersonalPaths() []string {
	if agent.HarnessFormat == "claude-code" {
		return []string{"CLAUDE.local.md"}
	}
	return nil
}

// Unplace records the request and removes everything but a foreign path.
func (agent *Agent) Unplace(placed machine.Placement) (removed bool, reason string, err error) {
	agent.Unplaced = append(agent.Unplaced, placed.Path)
	if strings.Contains(placed.Path, "foreign") {
		return false, "links elsewhere", nil
	}
	return true, "", nil
}

// State keeps the machine state in memory and fails the next loads or saves
// on demand, which is how a crash and a compensation get exercised.
type State struct {
	Current   machine.State
	FailLoads int
	FailSaves int
}

// NewState is an empty state.
func NewState() *State {
	return &State{Current: machine.Empty()}
}

// Load returns the state or fails when asked to.
func (state *State) Load() (machine.State, error) {
	if state.FailLoads > 0 {
		state.FailLoads--
		return machine.State{}, errors.New("state: cannot read state.yaml")
	}
	return state.Current, nil
}

// Save keeps the state or fails when asked to.
func (state *State) Save(updated machine.State) error {
	if state.FailSaves > 0 {
		state.FailSaves--
		return errors.New("state: disk full")
	}
	state.Current = updated
	return nil
}

// Judge answers from a script: each Ask takes the next answer; an empty
// list means the backend is not available.
type Judge struct {
	Answers     []string
	Unavailable string
	Briefs      []string
}

// Name is the fake's name.
func (*Judge) Name() string {
	return "fake-judge"
}

// Available is false with a reason when Unavailable is set.
func (judge *Judge) Available() (string, bool) {
	if judge.Unavailable != "" {
		return judge.Unavailable, false
	}
	return "", true
}

// Ask records the brief and returns the next scripted answer.
func (judge *Judge) Ask(brief string) (string, string, error) {
	judge.Briefs = append(judge.Briefs, brief)
	if len(judge.Answers) == 0 {
		return "", "", errors.New("fake judge has no answer left")
	}
	answer := judge.Answers[0]
	judge.Answers = judge.Answers[1:]
	return answer, "fake-session", nil
}

// Lock is a machine lock that is free, or held by someone else when Holder
// is set. It counts how often it was taken.
type Lock struct {
	Holder   string
	Acquired int
	Released int
}

// Acquire fails while Holder is set, else counts and returns a release.
func (lock *Lock) Acquire(wait time.Duration) (func(), error) {
	if lock.Holder != "" {
		return nil, errors.New("another rein is running (pid " + lock.Holder + " holds the lock); wait for it")
	}
	lock.Acquired++
	return func() { lock.Released++ }, nil
}

// Releases is a fake source of rein's releases: Latest_ is what Latest
// returns, Files what Download returns by URL, Fail an error for both.
type Releases struct {
	Latest_ ports.Release
	Files   map[string][]byte
	Fail    error
	Calls   int
}

// Latest returns the configured release.
func (releases *Releases) Latest() (ports.Release, error) {
	releases.Calls++
	if releases.Fail != nil {
		return ports.Release{}, releases.Fail
	}
	return releases.Latest_, nil
}

// Download returns the configured bytes for the URL.
func (releases *Releases) Download(url string) ([]byte, error) {
	if releases.Fail != nil {
		return nil, releases.Fail
	}
	content, found := releases.Files[url]
	if !found {
		return nil, errors.New("no such asset: " + url)
	}
	return content, nil
}
