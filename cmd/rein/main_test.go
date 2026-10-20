package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Danidani23/agents-rein/internal/app/0_gateway/cli"
)

// These are the end-to-end checks of the real wiring: adapters over a
// stubbed gh and claude on the PATH, a scratch org repository, a temp home.

func run(arguments ...string) (code int, out string, errOut string) {
	var outBuffer, errBuffer bytes.Buffer
	code = cli.Run(arguments, strings.NewReader(""), &outBuffer, &errBuffer, wire)
	return code, outBuffer.String(), errBuffer.String()
}

func isolate(t *testing.T, login string) (reinHome string) {
	t.Helper()
	reinHome = t.TempDir()
	t.Setenv("REIN_HOME", reinHome)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(t.TempDir(), ".claude"))
	directory := t.TempDir()
	remotes := filepath.Join(reinHome, "stub-remotes")
	_ = os.MkdirAll(remotes, 0o755)
	script := "#!/bin/sh\nREMOTES=\"" + remotes + "\"\ncase \"$*\" in\n  \"api user --jq .login\")"
	if login == "" {
		script += " echo 'gh: not logged in' >&2; exit 1;;\n"
	} else {
		script += " echo " + login + ";;\n"
	}
	script += "  \"config get git_protocol\") echo https;;\n" +
		"  \"repo create \"*) name=\"${3#*/}\"; git init -q --bare \"$REMOTES/$name.git\" && git --git-dir=\"$REMOTES/$name.git\" symbolic-ref HEAD refs/heads/main;;\n" +
		"  \"repo view \"*) name=\"${3#*/}\"; echo \"$REMOTES/$name.git\";;\n" +
		"  \"repo delete \"*) name=\"${3#*/}\"; rm -rf \"$REMOTES/$name.git\";;\n" +
		"  \"api -X PUT \"*) if [ \"$GH_STUB_PROTECTION\" = 403 ]; then echo 'gh: Upgrade to GitHub Pro or make this repository public to enable this feature. (HTTP 403)' >&2; exit 1; fi;;\n" +
		"  \"pr create \"*) echo https://github.com/Danidani23/acme-agent-settings/pull/1;;\n" +
		"  *) exit 1;;\nesac\n"
	err := os.WriteFile(filepath.Join(directory, "gh"), []byte(script), 0o755)
	if err != nil {
		t.Fatalf("stub gh: %v", err)
	}
	// the stub claude answers a judge brief with a canned verdict for the three rules of the init test
	claudeStub := "#!/bin/sh\nif [ \"$1\" = \"-p\" ]; then\n  cat >/dev/null\n  printf '%s' '" + stubJudgeAnswer + "'\nfi\nexit 0\n"
	err = os.WriteFile(filepath.Join(directory, "claude"), []byte(claudeStub), 0o755)
	if err != nil {
		t.Fatalf("stub claude: %v", err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	// git commits need an identity and must not read the machine's config
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
	return reinHome
}

// stubJudgeAnswer is claude's JSON envelope around a judge answer.
const stubJudgeAnswer = `{"result":"{\"version\":1,\"candidates\":[{\"candidate\":\"rules/go\",\"verdict\":\"new\",\"suggestion\":\"keep\",\"reason\":\"fine\"},{\"candidate\":\"rules/instructions\",\"verdict\":\"new\",\"suggestion\":\"keep\",\"reason\":\"fine\"}]}","is_error":false}`

func scratchOrgRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	git := func(arguments ...string) {
		command := exec.Command("git", arguments...)
		command.Dir = root
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", arguments, err, output)
		}
	}
	git("init", "-q")
	folder := filepath.Join(root, "declarations", "organizations", "acme")
	_ = os.MkdirAll(folder, 0o755)
	_ = os.WriteFile(filepath.Join(folder, "organization.yaml"), []byte("id: 0f8fad5b-d9cb-469f-a165-70867728950e\nslug: acme\nname: Acme\nhost: github\n"), 0o644)
	git("add", "declarations")
	git("-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "organisation record")
	return root
}

func TestWiring(t *testing.T) {
	t.Run("status shows the stubbed identity and agent", func(t *testing.T) {
		isolate(t, "Danidani23")
		code, out, _ := run("status")
		if code != cli.ExitWorked || !strings.Contains(out, "github      Danidani23") || !strings.Contains(out, "agents      claude-code") {
			t.Fatalf("code %d out %q", code, out)
		}
	})
	t.Run("a logged-out gh refuses org list and is shown by status", func(t *testing.T) {
		isolate(t, "")
		code, _, errOut := run("org", "list")
		if code != cli.ExitRefused || !strings.Contains(errOut, "gh auth login") {
			t.Fatalf("code %d err %q", code, errOut)
		}
		code, out, _ := run("status")
		if code != cli.ExitWorked || !strings.Contains(out, "github      none") {
			t.Fatalf("code %d out %q", code, out)
		}
	})
	t.Run("connect, list, disconnect and reset --all run through the real adapters", func(t *testing.T) {
		reinHome := isolate(t, "Danidani23")
		remote := scratchOrgRepository(t)
		code, out, errOut := run("org", "connect", remote)
		if code != cli.ExitWorked || !strings.Contains(out, "connected acme (Acme)") {
			t.Fatalf("connect: code %d out %q err %q", code, out, errOut)
		}
		_, err := os.Stat(filepath.Join(reinHome, "orgs", "0f8fad5b-d9cb-469f-a165-70867728950e", "declarations"))
		if err != nil {
			t.Fatalf("clone: %v", err)
		}
		code, out, _ = run("org", "list")
		if code != cli.ExitWorked || !strings.Contains(out, "* acme") {
			t.Fatalf("list: code %d out %q", code, out)
		}
		code, out, _ = run("reset", "--yes")
		if code != cli.ExitWorked || !strings.Contains(out, "removed  clone") {
			t.Fatalf("reset: code %d out %q", code, out)
		}
		_, err = os.Stat(filepath.Join(reinHome, "orgs"))
		if err == nil {
			t.Fatalf("orgs folder still there")
		}
	})
	t.Run("org create runs through the stubbed gh and real git, and snapshots the agent home", func(t *testing.T) {
		reinHome := isolate(t, "Danidani23")
		claudeHome := os.Getenv("CLAUDE_CONFIG_DIR")
		_ = os.MkdirAll(claudeHome, 0o755)
		_ = os.WriteFile(filepath.Join(claudeHome, "CLAUDE.md"), []byte("be brief"), 0o644)
		code, out, errOut := run("org", "create", "--name", "Acme", "--yes")
		remote := filepath.Join(reinHome, "stub-remotes", "acme-agent-settings.git")
		if code != cli.ExitWorked || !strings.Contains(out, "created Danidani23/acme-agent-settings (private) at "+remote) || !strings.Contains(out, "protected main") || !strings.Contains(out, "connected acme (Acme) at") || !strings.Contains(out, "snapshot of an agent's home: "+filepath.Join(reinHome, "snapshots")) {
			t.Fatalf("code %d out %q err %q", code, out, errOut)
		}
		entries, _ := os.ReadDir(filepath.Join(reinHome, "orgs"))
		if len(entries) != 1 || strings.HasPrefix(entries[0].Name(), ".create-") {
			t.Fatalf("orgs: %v", entries)
		}
		_, err := os.Stat(filepath.Join(reinHome, "orgs", entries[0].Name(), "CODEOWNERS"))
		if err != nil {
			t.Fatalf("clone: %v", err)
		}
		code, out, _ = run("org", "list")
		if code != cli.ExitWorked || !strings.Contains(out, "* acme  "+remote) {
			t.Fatalf("list: code %d out %q", code, out)
		}
		code, out, errOut = run("team", "create", "--name", "Web", "--yes")
		if code != cli.ExitWorked || !strings.Contains(out, "created team web (Web) in acme") {
			t.Fatalf("team: code %d out %q err %q", code, out, errOut)
		}
		code, out, errOut = run("project", "create", "--name", "Shop", "--team", "web", "--label", "language:go", "--yes")
		if code != cli.ExitWorked || !strings.Contains(out, "created project shop (Shop) in acme") {
			t.Fatalf("project: code %d out %q err %q", code, out, errOut)
		}
		code, out, errOut = run("resolve", "--project", "shop")
		if code != cli.ExitWorked || !strings.Contains(out, "labels language:go, user danidani23") || !strings.Contains(out, "no declarations apply yet") {
			t.Fatalf("resolve: code %d out %q err %q", code, out, errOut)
		}
		log := exec.Command("git", "--git-dir", remote, "rev-list", "--count", "main")
		count, err := log.Output()
		if err != nil || strings.TrimSpace(string(count)) != "3" {
			t.Fatalf("remote commits %q err %v", count, err)
		}
		// init on a real project repository: tracked agent files and an untracked one
		project, _ := filepath.EvalSymlinks(t.TempDir())
		git := func(arguments ...string) string {
			command := exec.Command("git", arguments...)
			command.Dir = project
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("git %v: %v\n%s", arguments, err, output)
			}
			return strings.TrimSpace(string(output))
		}
		git("init", "-q")
		_ = os.MkdirAll(filepath.Join(project, ".claude", "rules"), 0o755)
		_ = os.WriteFile(filepath.Join(project, "CLAUDE.md"), []byte("be brief"), 0o644)
		_ = os.WriteFile(filepath.Join(project, ".claude", "rules", "go.md"), []byte("rule"), 0o644)
		_ = os.WriteFile(filepath.Join(project, "main.go"), []byte("package main"), 0o644)
		git("add", "-A")
		git("commit", "-q", "-m", "first")
		_ = os.WriteFile(filepath.Join(project, "CLAUDE.local.md"), []byte("mine"), 0o644)
		code, out, errOut = run("init", "--path", project, "--project", "shop", "--yes")
		if code != cli.ExitWorked || !strings.Contains(out, "bound "+project+" to organizations.acme.projects.shop") || !strings.Contains(out, "moved out for claude-code: CLAUDE.md, .claude") || !strings.Contains(out, "committed ") {
			t.Fatalf("init: code %d out %q err %q", code, out, errOut)
		}
		for _, gone := range []string{"CLAUDE.md", ".claude"} {
			_, err = os.Stat(filepath.Join(project, gone))
			if err == nil {
				t.Fatalf("%s still in the clone", gone)
			}
		}
		// CLAUDE.local.md is the developer's: untouched, still untracked, now ignored by the block
		if git("rev-list", "--count", "HEAD") != "2" || git("status", "--porcelain") != "" || !strings.Contains(git("log", "-1", "--format=%s"), "move agent settings out to rein") {
			t.Fatalf("commit missing or tree dirty: %q", git("status", "--porcelain"))
		}
		_, err = os.Stat(filepath.Join(project, "CLAUDE.local.md"))
		if err != nil {
			t.Fatalf("CLAUDE.local.md was moved out")
		}
		code, out, errOut = run("sync", "--path", project)
		if code != cli.ExitWorked || !strings.Contains(out, "synced organizations.acme.projects.shop for claude-code") || !strings.Contains(out, "written    CLAUDE.md") || !strings.Contains(out, "written    .claude/settings.local.json") {
			t.Fatalf("sync: code %d out %q err %q", code, out, errOut)
		}
		generated, _ := os.ReadFile(filepath.Join(project, "CLAUDE.md"))
		settings, _ := os.ReadFile(filepath.Join(project, ".claude", "settings.local.json"))
		if !strings.HasPrefix(string(generated), "<!-- Generated by rein sync") || !strings.Contains(string(settings), "sync --hook") || git("status", "--porcelain") != "" {
			t.Fatalf("generated %q settings %q status %q", generated, settings, git("status", "--porcelain"))
		}
		insertsFiles, _ := os.ReadDir(filepath.Join(reinHome, "inserts"))
		if len(insertsFiles) != 1 {
			t.Fatalf("inserts %v", insertsFiles)
		}
		code, out, errOut = run("sync", "--path", project, "--hook")
		if code != cli.ExitWorked || !strings.Contains(out, "rein: organizations.acme.projects.shop synced for claude-code, 0 files written") {
			t.Fatalf("hook: code %d out %q err %q", code, out, errOut)
		}
		ignore, _ := os.ReadFile(filepath.Join(project, ".gitignore"))
		state, _ := os.ReadFile(filepath.Join(reinHome, "state.yaml"))
		removals, _ := os.ReadFile(filepath.Join(reinHome, "removals.yaml"))
		if !strings.Contains(string(ignore), "# >>> rein") || !strings.Contains(string(state), "organizations.acme.projects.shop") || !strings.Contains(string(removals), filepath.Join(project, "CLAUDE.md")) {
			t.Fatalf("ignore %q state %q removals %q", ignore, state, removals)
		}
		if git("--git-dir", remote, "rev-list", "--count", "main") != "4" {
			t.Fatalf("the binding was not pushed")
		}
		code, out, errOut = run("distill", "--path", project)
		if code != cli.ExitWorked || !strings.Contains(out, "distilled 2 candidates for organizations.acme.projects.shop") || !strings.Contains(out, "rules/go") || !strings.Contains(out, "written to "+filepath.Join(reinHome, "distill")) || !strings.Contains(out, "judge: agent-cli:claude answered") {
			t.Fatalf("distill: code %d out %q err %q", code, out, errOut)
		}
		distilled := ""
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(line, "written to ") {
				distilled = strings.TrimPrefix(line, "written to ")
			}
		}
		code, out, errOut = run("propose", distilled, "--yes")
		if code != cli.ExitWorked || !strings.Contains(out, "proposed 2 declarations to organizations.acme.projects.shop on branch rein/propose-shop-") || !strings.Contains(out, "pull request: https://github.com/Danidani23/acme-agent-settings/pull/1") {
			t.Fatalf("propose: code %d out %q err %q", code, out, errOut)
		}
		branches := exec.Command("git", "--git-dir", remote, "branch", "--list", "rein/propose-*")
		listed, err := branches.Output()
		if err != nil || !strings.Contains(string(listed), "rein/propose-shop-") {
			t.Fatalf("remote branches %q err %v", listed, err)
		}
		clone := filepath.Join(reinHome, "orgs", entries[0].Name())
		head := exec.Command("git", "-C", clone, "rev-parse", "--abbrev-ref", "HEAD")
		current, err := head.Output()
		if err != nil || strings.TrimSpace(string(current)) != "main" {
			t.Fatalf("clone on %q err %v", current, err)
		}
		// restore puts the project back and commits; the untracked local file returns untracked
		code, out, errOut = run("restore", "--path", project, "--yes")
		if code != cli.ExitWorked || !strings.Contains(out, "removed sync's files: .claude/settings.local.json, CLAUDE.md") || !strings.Contains(out, "put back: .claude/rules/go.md, CLAUDE.md") || !strings.Contains(out, ".gitignore: rein block removed") || !strings.Contains(out, "committed ") {
			t.Fatalf("restore: code %d out %q err %q", code, out, errOut)
		}
		_, err = os.Stat(filepath.Join(project, ".claude", "rein"))
		if err == nil {
			t.Fatalf("sync's folder stayed after restore")
		}
		if git("rev-list", "--count", "HEAD") != "3" || git("status", "--porcelain") != "?? CLAUDE.local.md" || !strings.Contains(git("log", "-1", "--format=%s"), "restore agent settings from rein") {
			t.Fatalf("after restore: count %s status %q", git("rev-list", "--count", "HEAD"), git("status", "--porcelain"))
		}
		content, _ := os.ReadFile(filepath.Join(project, "CLAUDE.md"))
		if string(content) != "be brief" {
			t.Fatalf("CLAUDE.md %q", content)
		}
		// reset wipes the rest
		code, out, errOut = run("reset", "--yes")
		if code != cli.ExitWorked || !strings.Contains(out, "removed  clone") || !strings.Contains(out, "removals.yaml") {
			t.Fatalf("reset: code %d out %q err %q", code, out, errOut)
		}
		for _, gone := range []string{"orgs", "snapshots", "distill", "removals.yaml", "inserts"} {
			_, err = os.Stat(filepath.Join(reinHome, gone))
			if err == nil {
				t.Fatalf("%s still there after reset", gone)
			}
		}
		state, _ = os.ReadFile(filepath.Join(reinHome, "state.yaml"))
		if !strings.HasPrefix(string(state), "# Written by rein") || !strings.Contains(string(state), "version: 1") || strings.Contains(string(state), "acme") {
			t.Fatalf("state %q", state)
		}
	})
	t.Run("org create reports a protection GitHub refuses and still connects", func(t *testing.T) {
		isolate(t, "Danidani23")
		t.Setenv("GH_STUB_PROTECTION", "403")
		code, out, errOut := run("org", "create", "--name", "Acme", "--yes")
		if code != cli.ExitWorked || !strings.Contains(errOut, "warning: main is not protected; GitHub said: gh: Upgrade to GitHub Pro") || !strings.Contains(out, "connected acme (Acme)") {
			t.Fatalf("code %d out %q err %q", code, out, errOut)
		}
	})
	t.Run("a remote that cannot be cloned exits 3", func(t *testing.T) {
		isolate(t, "Danidani23")
		code, _, errOut := run("org", "connect", filepath.Join(t.TempDir(), "nowhere"))
		if code != cli.ExitToolFailed || !strings.Contains(errOut, "git clone") {
			t.Fatalf("code %d err %q", code, errOut)
		}
	})
}
