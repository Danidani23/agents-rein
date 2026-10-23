package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// isolate keeps git from reading the machine's configuration and gives the
// commits an identity.
func isolate(t *testing.T) {
	t.Helper()
	emptyConfig := filepath.Join(t.TempDir(), "gitconfig")
	_ = os.WriteFile(emptyConfig, nil, 0o644)
	t.Setenv("GIT_CONFIG_GLOBAL", emptyConfig)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, variable := range []string{"GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME"} {
		t.Setenv(variable, "test")
	}
	for _, variable := range []string{"GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL"} {
		t.Setenv(variable, "test@example.com")
	}
}

func git(t *testing.T, directory string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", arguments, err, output)
	}
	return strings.TrimSpace(string(output))
}

// repository makes a clone on main of a bare origin, with one commit.
func repository(t *testing.T) (clone string, origin string) {
	t.Helper()
	origin = filepath.Join(t.TempDir(), "origin.git")
	git(t, t.TempDir(), "init", "-q", "--bare", "--initial-branch=main", origin)
	clone = filepath.Join(t.TempDir(), "clone")
	git(t, t.TempDir(), "clone", "-q", origin, clone)
	git(t, clone, "checkout", "-q", "-b", "main")
	commitFile(t, clone, "README.md", "hello\n", "chore: start")
	git(t, clone, "push", "-q", "-u", "origin", "main")
	return clone, origin
}

func commitFile(t *testing.T, clone string, name string, content string, message string) {
	t.Helper()
	_ = os.WriteFile(filepath.Join(clone, name), []byte(content), 0o644)
	git(t, clone, "add", name)
	git(t, clone, "commit", "-q", "-m", message)
}

func TestVersionsAndClassification(t *testing.T) {
	latest, found := latestRelease([]string{"v0.1.0", "v0.10.0", "v0.2.0", "nope", "v1.0"})
	if !found || latest.String() != "v0.10.0" {
		t.Fatalf("latest %s found %v", latest, found)
	}
	if _, found := latestRelease(nil); found {
		t.Fatalf("a release among no tags")
	}
	grouped := classify([]commit{
		{hash: "a1", subject: "feat(rein): a feature"},
		{hash: "b2", subject: "fix: a fix"},
		{hash: "c3", subject: "perf(sync): faster"},
		{hash: "d4", subject: "refactor!: breaks", body: ""},
		{hash: "e5", subject: "feat: footer", body: "BREAKING CHANGE: it does"},
		{hash: "f6", subject: "docs: words"},
		{hash: "g7", subject: "not conventional at all"},
	})
	counted := grouped.tally()
	if counted.breaking != 2 || counted.feat != 1 || counted.fix != 1 || counted.perf != 1 || counted.other != 2 || !counted.releasable() {
		t.Fatalf("tally %s", counted)
	}
	if nextVersion(version{0, 3, 2}, counted).String() != "v1.0.0" || nextVersion(version{0, 3, 2}, tally{feat: 1}).String() != "v0.4.0" || nextVersion(version{0, 3, 2}, tally{fix: 1}).String() != "v0.3.3" {
		t.Fatalf("next versions")
	}
	if (tally{other: 4}).releasable() {
		t.Fatalf("docs release")
	}
	text := section(version{0, 4, 0}, "2026-10-22", grouped)
	for _, expected := range []string{"## [0.4.0] - 2026-10-22\n", "### ⚠ BREAKING CHANGES\n\n- breaks (d4)\n- footer (e5)\n", "### Features\n\n- **rein:** a feature (a1)\n", "### Bug Fixes\n\n- a fix (b2)\n", "### Performance\n\n- **sync:** faster (c3)\n"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("missing %q in %q", expected, text)
		}
	}
	if strings.Contains(text, "words") || strings.Contains(text, "conventional") {
		t.Fatalf("other commits in the section: %q", text)
	}
	first := prependSection("", "## [0.1.0] - 2026-10-22\n")
	second := prependSection(first, "## [0.2.0] - 2026-10-23\n")
	if !strings.HasPrefix(first, "# Changelog\n\n## [0.1.0]") || !strings.HasPrefix(second, "# Changelog\n\n## [0.2.0] - 2026-10-23\n\n## [0.1.0]") {
		t.Fatalf("prepend %q then %q", first, second)
	}
}

func TestReleaseOverABareOrigin(t *testing.T) {
	isolate(t)
	gateCommands = [][]string{{"true"}}
	publishCommand = []string{"sh", "-c", "test \"$GITHUB_TOKEN\" = the-token && test -z \"$HOMEBREW_TAP_GITHUB_TOKEN\""}
	tokenCommand = []string{"echo", "the-token"}
	repositoryExists = func(root string, fullName string) bool { return fullName == tapRepository }
	defer func() {
		gateCommands = [][]string{{"task", "rein:check"}, {"task", "rein:release-check"}}
		publishCommand = defaultPublishCommand()
		tokenCommand = []string{"gh", "auth", "token"}
	}()
	clone, origin := repository(t)
	now := time.Date(2026, 10, 22, 18, 0, 0, 0, time.UTC)
	var out bytes.Buffer
	err := release(&out, clone, now)
	if err == nil || !strings.Contains(err.Error(), "nothing to release since the start: 0 breaking, 0 feat, 0 fix, 0 perf, 1 other") {
		t.Fatalf("a chore alone released: %v", err)
	}
	commitFile(t, clone, "feature.txt", "x\n", "feat(rein): the first feature")
	commitFile(t, clone, "note.txt", "x\n", "docs: a note")
	err = release(&out, clone, now)
	if err == nil || !strings.Contains(err.Error(), "main is not what origin/main has") {
		t.Fatalf("an unpushed main released: %v", err)
	}
	git(t, clone, "push", "-q", "origin", "main")
	_ = os.WriteFile(filepath.Join(clone, "dirty.txt"), []byte("x"), 0o644)
	err = release(&out, clone, now)
	if err == nil || !strings.Contains(err.Error(), "not clean") || !strings.Contains(err.Error(), "dirty.txt") {
		t.Fatalf("a dirty tree released: %v", err)
	}
	_ = os.Remove(filepath.Join(clone, "dirty.txt"))
	out.Reset()
	err = release(&out, clone, now)
	if err != nil {
		t.Fatalf("release: %v\n%s", err, out.String())
	}
	text := out.String()
	for _, expected := range []string{"releasing v0.1.0 (since the start: 0 breaking, 1 feat, 0 fix, 0 perf, 2 other)", "## [0.1.0] - 2026-10-22", "- **rein:** the first feature (", "running the gate: true", "the cask and the Scoop manifest are skipped", "building every platform and publishing the GitHub Release: sh -c", "released v0.1.0: main and the tag are pushed, the binaries are on the GitHub Release"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("missing %q in %q", expected, text)
		}
	}
	if !strings.HasSuffix(text, "\nv0.1.0\n") {
		t.Fatalf("the version is not the last line: %q", text)
	}
	if git(t, origin, "tag", "-l", "v0.1.0") != "v0.1.0" || git(t, origin, "log", "-1", "--format=%s", "main") != "chore(release): v0.1.0" {
		t.Fatalf("origin: tags %q main %q", git(t, origin, "tag", "-l"), git(t, origin, "log", "-1", "--format=%s", "main"))
	}
	changelog, _ := os.ReadFile(filepath.Join(clone, "CHANGELOG.md"))
	if !strings.HasPrefix(string(changelog), "# Changelog\n\n## [0.1.0] - 2026-10-22\n\n### Features\n\n- **rein:** the first feature (") {
		t.Fatalf("changelog %q", changelog)
	}
	// the second release: a fix bumps the patch, the section goes on top, the tag is refused twice
	commitFile(t, clone, "fix.txt", "x\n", "fix: a fix")
	git(t, clone, "push", "-q", "origin", "main")
	out.Reset()
	err = release(&out, clone, now.Add(24*time.Hour))
	if err != nil || !strings.Contains(out.String(), "releasing v0.1.1 (since v0.1.0: 0 breaking, 0 feat, 1 fix, 0 perf, 0 other)") {
		t.Fatalf("second release: err %v out %q", err, out.String())
	}
	changelog, _ = os.ReadFile(filepath.Join(clone, "CHANGELOG.md"))
	if !strings.HasPrefix(string(changelog), "# Changelog\n\n## [0.1.1] - 2026-10-23\n\n### Bug Fixes\n\n- a fix (") || !strings.Contains(string(changelog), "## [0.1.0] - 2026-10-22") {
		t.Fatalf("changelog after the second release %q", changelog)
	}
	git(t, clone, "checkout", "-q", "-b", "feature")
	err = release(&out, clone, now)
	if err == nil || !strings.Contains(err.Error(), "releases are cut from main, not feature") {
		t.Fatalf("off main: %v", err)
	}
	// a red gate releases nothing
	git(t, clone, "checkout", "-q", "main")
	commitFile(t, clone, "feature2.txt", "x\n", "feat: another")
	git(t, clone, "push", "-q", "origin", "main")
	gateCommands = [][]string{{"false"}}
	err = release(&out, clone, now)
	if err == nil || !strings.Contains(err.Error(), "the gate is red") || git(t, origin, "tag", "-l", "v0.2.0") != "" {
		t.Fatalf("red gate: %v tags %q", err, git(t, origin, "tag", "-l"))
	}
	changelog, _ = os.ReadFile(filepath.Join(clone, "CHANGELOG.md"))
	if strings.Contains(string(changelog), "0.2.0") {
		t.Fatalf("the changelog changed under a red gate")
	}
	// a failed publish leaves the tag pushed and says how to finish; both doors open hand the tap token over
	gateCommands = [][]string{{"true"}}
	publishCommand = []string{"false"}
	err = release(&out, clone, now)
	if err == nil || !strings.Contains(err.Error(), "v0.2.0 is tagged and pushed, but the publish failed; run reinctl publish") || git(t, origin, "tag", "-l", "v0.2.0") != "v0.2.0" {
		t.Fatalf("failed publish: %v tags %q", err, git(t, origin, "tag", "-l"))
	}
	repositoryExists = func(root string, fullName string) bool { return true }
	publishCommand = []string{"sh", "-c", "test \"$HOMEBREW_TAP_GITHUB_TOKEN\" = the-token"}
	out.Reset()
	err = publish(&out, clone, "the-token")
	if err != nil || strings.Contains(out.String(), "skipped") {
		t.Fatalf("publish with both doors: err %v out %q", err, out.String())
	}
	tokenCommand = []string{"sh", "-c", "exit 1"}
	_, err = gitHubToken(clone)
	if err == nil || !strings.Contains(err.Error(), "gh auth login") {
		t.Fatalf("no token: %v", err)
	}
}

func TestRunExitCodes(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"--help"}, &out, &errOut); code != exitWorked || !strings.Contains(out.String(), "release") {
		t.Fatalf("help: %d %q", code, out.String())
	}
	if code := run([]string{"nope"}, &out, &errOut); code != exitCrashed || !strings.Contains(errOut.String(), "reinctl:") {
		t.Fatalf("unknown command: %d %q", code, errOut.String())
	}
}

// TestBuildOutputIsIgnored guards the next release: GoReleaser writes its
// archives under dist/, and an untracked dist/ would make release refuse
// the tree as unclean.
func TestBuildOutputIsIgnored(t *testing.T) {
	ignore, err := os.ReadFile(filepath.Join("..", "..", ".gitignore"))
	if err != nil {
		t.Fatalf("gitignore: %v", err)
	}
	// a Windows checkout ends each line with \r\n
	ignoreLines := strings.ReplaceAll(string(ignore), "\r\n", "\n")
	if !strings.Contains("\n"+ignoreLines, "\n/dist/\n") {
		t.Fatalf(".gitignore does not ignore /dist/, where the publish writes its archives")
	}
}

func TestGoReleaserIsPinnedToOneVersion(t *testing.T) {
	module, version, found := strings.Cut(publishCommand[2], "@")
	if !found || module != "github.com/goreleaser/goreleaser/v2" || !strings.HasPrefix(version, "v2.") {
		t.Fatalf("the publish runs %q; it must run github.com/goreleaser/goreleaser/v2 at an exact v2 version, never @latest, so a new GoReleaser cannot break a release", publishCommand[2])
	}
	taskfile, err := os.ReadFile(filepath.Join("..", "..", "Taskfile.yml"))
	if err != nil {
		t.Fatalf("Taskfile: %v", err)
	}
	check := "go run github.com/goreleaser/goreleaser/v2@" + version + " check"
	if !strings.Contains(string(taskfile), check) {
		t.Fatalf("Taskfile.yml does not run %q; the gate must check the configuration with the GoReleaser that publishes", check)
	}
}
