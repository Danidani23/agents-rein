package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/adapters/execute"
)

// gateCommands run before anything changes; a red one releases nothing.
// A test replaces them.
var gateCommands = [][]string{{"task", "rein:check"}, {"task", "rein:release-check"}}

// goReleaserVersion is the one GoReleaser that checks the configuration in
// the gate and publishes the release; Taskfile.yml names the same version,
// and a test keeps the two together. Raising it: change both, run
// task rein:release-check, and follow its deprecation notices.
const goReleaserVersion = "v2.19.0"

// publishCommand builds every platform and publishes the GitHub Release
// for the tag at HEAD; a test replaces it.
var publishCommand = defaultPublishCommand()

func defaultPublishCommand() []string {
	return []string{"go", "run", "github.com/goreleaser/goreleaser/v2@" + goReleaserVersion, "release", "--clean"}
}

// tokenCommand prints the token gh holds for GitHub; a test replaces it.
var tokenCommand = []string{"gh", "auth", "token"}

// repositoryExists says whether owner/name is on GitHub; a test replaces
// it.
var repositoryExists = func(root string, fullName string) bool {
	_, err := capture(root, "gh", "repo", "view", fullName)
	return err == nil
}

// The repositories the cask and the manifest are written into.
const (
	tapRepository    = "Danidani23/homebrew-tap"
	bucketRepository = "Danidani23/scoop-bucket"
)

// door is a package channel written into a GitHub repository beside the
// GitHub Release: open when the repository exists, through the variable
// .goreleaser.yaml reads (D30).
type door struct {
	name       string
	repository string
	variable   string
}

var doors = []door{
	{name: "the Homebrew cask", repository: tapRepository, variable: "REIN_TAP_TOKEN"},
	{name: "the Scoop manifest", repository: bucketRepository, variable: "REIN_BUCKET_TOKEN"},
	{name: "the winget manifest", repository: "Danidani23/winget-pkgs", variable: "REIN_WINGET_TOKEN"},
	{name: "the Nix package", repository: "Danidani23/nur-packages", variable: "REIN_NUR_TOKEN"},
}

// aurKey is the SSH key the AUR knows the maintainer by; the AUR package
// is pushed only when it exists. A test replaces it.
var aurKey = defaultAURKey()

func defaultAURKey() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ssh", "aur")
}

// productRepository is rein's own repository, whose check workflow must
// have passed on the commit a release is cut from (D29).
const (
	productRepository = "Danidani23/agents-rein"
	checkWorkflow     = "check.yml"
)

// checkRunOn says how the latest run of the check workflow on a commit
// stands: an empty status when it never ran; a test replaces it.
var checkRunOn = gitHubCheckRunOn

func gitHubCheckRunOn(root string, commit string) (status string, conclusion string, err error) {
	runs := fmt.Sprintf("repos/%s/actions/workflows/%s/runs?head_sha=%s&per_page=1", productRepository, checkWorkflow, commit)
	latest, err := capture(root, "gh", "api", runs, "--jq", `.workflow_runs[0] // empty | .status + " " + (.conclusion // "")`)
	if err != nil {
		return "", "", fmt.Errorf("asking GitHub about %s on %s: %w", checkWorkflow, commit, err)
	}
	status, conclusion, _ = strings.Cut(latest, " ")
	return status, conclusion, nil
}

// releaseBranch is the one branch releases are cut from.
const releaseBranch = "main"

// newReleaseCommand is `reinctl release`: everything from this machine,
// nothing on the host. Every precondition and the gate run before a single
// file changes; the changelog, the commit and the tag are local and happen
// together; then the pushes, then the publish: every platform built here
// and the GitHub Release made with the token gh holds.
func newReleaseCommand(out io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "release",
		Short: "Cut the next version on main: gate, changelog, commit, tag, push; the human runs this",
		Long: "release reads the conventional commits since the last tag: a breaking change raises the\n" +
			"major version, a feat the minor, a fix or a perf the patch; anything else releases nothing.\n" +
			"It refuses off main, on a dirty tree, when main is not what origin has, when GitHub's\n" +
			"check.yml has not passed on that commit, and when the tag exists. The gate runs before\n" +
			"anything changes. Then CHANGELOG.md gets its section, the commit chore(release): vX.Y.Z\n" +
			"and the annotated tag are made, and main and the tag are pushed. Then GoReleaser builds\n" +
			"every platform and the Linux packages on this machine and publishes the GitHub Release\n" +
			"with the token gh holds; the Homebrew, Scoop, winget, Nix and AUR packages are written\n" +
			"when the repository or the key each needs exists. A publish that failed after the tag\n" +
			"was pushed is run again with reinctl publish.",
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			root, err := repositoryRoot()
			if err != nil {
				return err
			}
			return release(out, root, time.Now())
		},
	}
}

// release runs the verb in the repository at root.
func release(out io.Writer, root string, now time.Time) error {
	for _, tool := range []string{"git", "task", "gh", "go"} {
		err := execute.Preflight(tool)
		if err != nil {
			return refuse("%s is needed: %v", tool, err)
		}
	}
	token, err := gitHubToken(root)
	if err != nil {
		return err
	}
	branch, err := capture(root, "git", "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return err
	}
	if branch != releaseBranch {
		return refuse("releases are cut from %s, not %s", releaseBranch, branch)
	}
	dirty, err := capture(root, "git", "status", "--porcelain")
	if err != nil {
		return err
	}
	if dirty != "" {
		return refuse("the working tree is not clean; a release describes exactly one commit:\n%s", dirty)
	}
	remotes, err := capture(root, "git", "remote")
	if err != nil {
		return err
	}
	if !contains(strings.Fields(remotes), "origin") {
		return refuse("the repository has no remote named origin to push the release to")
	}
	fmt.Fprintf(out, "fetching origin %s\n", releaseBranch)
	_, err = capture(root, "git", "fetch", "origin", releaseBranch)
	if err != nil {
		return err
	}
	head, err := capture(root, "git", "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	originHead, err := capture(root, "git", "rev-parse", "origin/"+releaseBranch)
	if err != nil {
		return err
	}
	if head != originHead {
		return refuse("%s is not what origin/%s has; push or pull first", releaseBranch, releaseBranch)
	}
	fmt.Fprintf(out, "asking GitHub how %s stands on %s\n", checkWorkflow, head)
	status, conclusion, err := checkRunOn(root, head)
	if err != nil {
		return err
	}
	switch {
	case status == "":
		return refuse("%s has not run on %s; a release needs it green on Linux, macOS and Windows", checkWorkflow, head)
	case status != "completed":
		return refuse("%s is %s on %s; release once it has passed", checkWorkflow, status, head)
	case conclusion != "success":
		return refuse("%s ended %s on %s; a release needs it green on Linux, macOS and Windows", checkWorkflow, conclusion, head)
	}
	plan, err := planRelease(root, now)
	if err != nil {
		return err
	}
	counted := plan.groups.tally()
	if !counted.releasable() {
		return refuse("nothing to release since %s: %s; only breaking, feat, fix and perf release", plan.lastLabel(), counted)
	}
	tag := plan.next.String()
	fmt.Fprintf(out, "\nreleasing %s (since %s: %s)\n\n%s\n", tag, plan.lastLabel(), counted, plan.section)
	local, err := capture(root, "git", "tag", "-l", tag)
	if err != nil {
		return err
	}
	if local != "" {
		return refuse("tag %s already exists locally; versions are immutable", tag)
	}
	onOrigin, err := capture(root, "git", "ls-remote", "--tags", "origin", "refs/tags/"+tag)
	if err != nil {
		return err
	}
	if onOrigin != "" {
		return refuse("tag %s already exists on origin; versions are immutable", tag)
	}
	for _, gate := range gateCommands {
		fmt.Fprintf(out, "running the gate: %s\n", strings.Join(gate, " "))
		_, err = execute.RunWith(execute.Options{Directory: root, Timeout: execute.NetworkTimeout}, gate[0], gate[1:]...)
		if err != nil {
			return fmt.Errorf("the gate is red, nothing is released: %w", err)
		}
	}
	changelog := filepath.Join(root, "CHANGELOG.md")
	existing, err := os.ReadFile(changelog)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	err = os.WriteFile(changelog, []byte(prependSection(string(existing), plan.section)), 0o644)
	if err != nil {
		return err
	}
	for _, step := range [][]string{
		{"git", "add", "CHANGELOG.md"},
		{"git", "commit", "-q", "-m", "chore(release): " + tag},
		{"git", "tag", "-a", tag, "-m", "Release " + tag},
	} {
		_, err = capture(root, step[0], step[1:]...)
		if err != nil {
			return err
		}
	}
	fmt.Fprintf(out, "pushing %s\n", releaseBranch)
	_, err = capture(root, "git", "push", "-q", "origin", releaseBranch)
	if err == nil {
		fmt.Fprintf(out, "pushing the tag %s\n", tag)
		_, err = capture(root, "git", "push", "-q", "origin", tag)
	}
	if err != nil {
		return fmt.Errorf("%s is committed and tagged locally, but a push failed; finish with: git push origin %s %s, then reinctl publish: %w", tag, releaseBranch, tag, err)
	}
	err = publish(out, root, token)
	if err != nil {
		return fmt.Errorf("%s is tagged and pushed, but the publish failed; run reinctl publish once the cause is fixed: %w", tag, err)
	}
	fmt.Fprintf(out, "\nreleased %s: %s and the tag are pushed, the binaries are on the GitHub Release\n", tag, releaseBranch)
	// the version alone, last, so a shell can capture it
	fmt.Fprintln(out, tag)
	return nil
}

// gitHubToken is the token gh holds, for the publish only; it is handed to
// GoReleaser's environment and never written anywhere.
func gitHubToken(root string) (string, error) {
	token, err := capture(root, tokenCommand[0], tokenCommand[1:]...)
	if err != nil || token == "" {
		return "", refuse("gh holds no token for GitHub; run gh auth login first")
	}
	return token, nil
}

// publish builds every platform on this machine and makes the GitHub
// Release for the tag at HEAD, each package channel included when what it
// needs exists.
func publish(out io.Writer, root string, token string) error {
	environment := []string{"GITHUB_TOKEN=" + token}
	for _, door := range doors {
		if !repositoryExists(root, door.repository) {
			fmt.Fprintf(out, "%s is skipped: %s does not exist on GitHub\n", door.name, door.repository)
			continue
		}
		environment = append(environment, door.variable+"="+token)
	}
	_, err := os.Stat(aurKey)
	if err != nil {
		fmt.Fprintf(out, "the AUR package is skipped: no SSH key at %s\n", aurKey)
	} else {
		environment = append(environment, "REIN_AUR_KEY="+aurKey)
	}
	fmt.Fprintf(out, "building every platform and publishing the GitHub Release: %s\n", strings.Join(publishCommand, " "))
	_, err = execute.RunWith(execute.Options{Directory: root, Timeout: execute.NetworkTimeout, Environment: environment}, publishCommand[0], publishCommand[1:]...)
	return err
}

// newPublishCommand is `reinctl publish`: the publish step alone, for a
// release whose tag is pushed but whose publish failed.
func newPublishCommand(out io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "publish",
		Short: "Build every platform and publish the GitHub Release for the tag at HEAD; for a release whose publish failed",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			root, err := repositoryRoot()
			if err != nil {
				return err
			}
			for _, tool := range []string{"git", "gh", "go"} {
				err := execute.Preflight(tool)
				if err != nil {
					return refuse("%s is needed: %v", tool, err)
				}
			}
			tag, err := capture(root, "git", "tag", "--points-at", "HEAD", "v*")
			if err != nil {
				return err
			}
			if tag == "" {
				return refuse("HEAD carries no release tag; reinctl release makes one")
			}
			token, err := gitHubToken(root)
			if err != nil {
				return err
			}
			err = publish(out, root, token)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "\npublished %s\n", tag)
			return nil
		},
	}
}

// repositoryRoot is the top folder of the repository the command runs in.
func repositoryRoot() (string, error) {
	root, err := capture("", "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return "", refuse("not inside a git repository: %v", err)
	}
	return root, nil
}

func contains(items []string, item string) bool {
	for _, candidate := range items {
		if candidate == item {
			return true
		}
	}
	return false
}
