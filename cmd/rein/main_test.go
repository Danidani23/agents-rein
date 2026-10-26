package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Danidani23/agents-rein/internal/app/0_gateway/cli"
)

// These are the end-to-end checks of the real wiring: adapters over a
// stubbed gh and claude on the PATH, a scratch org repository, a temp home.

// claudeStubFolder holds this test binary under the name claude, which
// isolate puts first on the PATH.
var claudeStubFolder string

// TestMain makes the stub claude a copy of this test binary, because Windows
// runs only executables and no shell script. Started as claude, the binary
// answers a judge brief with the canned verdict for the rules of the init test.
func TestMain(tests *testing.M) {
	if strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe") == "claude" {
		if len(os.Args) > 1 && os.Args[1] == "-p" {
			_, _ = io.Copy(io.Discard, os.Stdin)
			// the answer streams as one text delta, and the result ends the stream
			delta, _ := json.Marshal(map[string]any{"type": "stream_event", "event": map[string]any{"type": "content_block_delta", "delta": map[string]any{"type": "text_delta", "text": stubJudgeAnswer}}})
			final, _ := json.Marshal(map[string]any{"type": "result", "subtype": "success", "is_error": false, "result": stubJudgeAnswer})
			fmt.Printf("%s\n%s\n", delta, final)
		}
		// the arguments it was started with, for a test to read
		if recorded := os.Getenv("REIN_TEST_CLAUDE_ARGUMENTS"); recorded != "" {
			_ = os.WriteFile(recorded, []byte(strings.Join(os.Args[1:], " ")), 0o644)
		}
		os.Exit(0)
	}
	folder, err := claudeStub()
	if err != nil {
		fmt.Fprintf(os.Stderr, "stub claude: %v\n", err)
		os.Exit(1)
	}
	claudeStubFolder = folder
	code := tests.Run()
	_ = os.RemoveAll(folder)
	os.Exit(code)
}

// claudeStub copies this test binary into a new folder as claude.
func claudeStub() (folder string, err error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	content, err := os.ReadFile(executable)
	if err != nil {
		return "", err
	}
	folder, err = os.MkdirTemp("", "rein-claude-stub")
	if err != nil {
		return "", err
	}
	name := "claude"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	err = os.WriteFile(filepath.Join(folder, name), content, 0o755)
	if err != nil {
		return "", err
	}
	return folder, nil
}

func run(arguments ...string) (code int, out string, errOut string) {
	var outBuffer, errBuffer bytes.Buffer
	code = cli.Run(arguments, strings.NewReader(""), &outBuffer, &errBuffer, wire)
	return code, outBuffer.String(), errBuffer.String()
}

// gitHubServer is a GitHub that answers what rein asks: the device flow,
// approved at the first poll; the user and its organisations; repositories
// as bare git repositories under remotes, so real git pushes into them.
func gitHubServer(t *testing.T, login string, remotes string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		answer := func(status int, body any) {
			writer.WriteHeader(status)
			_ = json.NewEncoder(writer).Encode(body)
		}
		bare := func(name string) string { return filepath.Join(remotes, name+".git") }
		parts := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
		switch {
		case request.URL.Path == "/login/device/code":
			answer(http.StatusOK, map[string]any{"device_code": "d", "user_code": "ABCD-1234", "verification_uri": "https://github.com/login/device", "expires_in": 900, "interval": 1})
		case request.URL.Path == "/login/oauth/access_token":
			if login == "" {
				answer(http.StatusOK, map[string]any{"error": "access_denied"})
				return
			}
			answer(http.StatusOK, map[string]any{"access_token": "ghu_" + login, "refresh_token": "r", "expires_in": 28800})
		case request.URL.Path == "/user":
			if request.Header.Get("Authorization") != "Bearer ghu_"+login {
				answer(http.StatusUnauthorized, map[string]any{"message": "Bad credentials"})
				return
			}
			answer(http.StatusOK, map[string]any{"login": login})
		case request.URL.Path == "/user/orgs":
			answer(http.StatusOK, []any{})
		case request.URL.Path == "/user/repos" && request.Method == http.MethodPost:
			var body map[string]any
			_ = json.NewDecoder(request.Body).Decode(&body)
			name := body["name"].(string)
			for _, arguments := range [][]string{{"init", "-q", "--bare", bare(name)}, {"--git-dir", bare(name), "symbolic-ref", "HEAD", "refs/heads/main"}} {
				output, err := exec.Command("git", arguments...).CombinedOutput()
				if err != nil {
					answer(http.StatusInternalServerError, map[string]any{"message": string(output)})
					return
				}
			}
			answer(http.StatusCreated, map[string]any{"clone_url": bare(name)})
		case len(parts) == 3 && parts[0] == "repos":
			_, err := os.Stat(bare(parts[2]))
			if err != nil {
				answer(http.StatusNotFound, map[string]any{"message": "Not Found"})
				return
			}
			answer(http.StatusOK, map[string]any{"clone_url": bare(parts[2])})
		case len(parts) == 4 && parts[0] == "repos" && parts[3] == "commits":
			err := exec.Command("git", "--git-dir", bare(parts[2]), "rev-parse", "HEAD").Run()
			if err != nil {
				answer(http.StatusConflict, map[string]any{"message": "Git Repository is empty."})
				return
			}
			answer(http.StatusOK, []map[string]any{{"sha": "x"}})
		case strings.HasSuffix(request.URL.Path, "/branches/main/protection"):
			if os.Getenv("GH_STUB_PROTECTION") == "403" {
				answer(http.StatusForbidden, map[string]any{"message": "Upgrade to GitHub Pro or make this repository public to enable this feature."})
				return
			}
			answer(http.StatusOK, map[string]any{})
		case strings.HasSuffix(request.URL.Path, "/pulls"):
			answer(http.StatusCreated, map[string]any{"html_url": "https://github.com/" + parts[1] + "/" + parts[2] + "/pull/1"})
		default:
			answer(http.StatusNotFound, map[string]any{"message": "no such endpoint in the test server: " + request.URL.Path})
		}
	}))
}

// isolate gives a test its own rein home, agent home, fake GitHub, file
// keychain and stub claude, and logs in when a login is given.
func isolate(t *testing.T, login string) (reinHome string) {
	t.Helper()
	reinHome = t.TempDir()
	t.Setenv("REIN_HOME", reinHome)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(t.TempDir(), ".claude"))
	t.Setenv("REIN_KEYCHAIN_FILE", filepath.Join(reinHome, "keychain.json"))
	remotes := filepath.Join(reinHome, "stub-remotes")
	_ = os.MkdirAll(remotes, 0o755)
	server := gitHubServer(t, login, remotes)
	t.Cleanup(server.Close)
	t.Setenv("REIN_GITHUB_WEB", server.URL)
	t.Setenv("REIN_GITHUB_API", server.URL)
	t.Setenv("PATH", claudeStubFolder+string(os.PathListSeparator)+os.Getenv("PATH"))
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
	if login != "" {
		code, out, errOut := run("login")
		if code != cli.ExitWorked || !strings.Contains(out, "enter the code ABCD-1234") || !strings.Contains(out, "logged in as "+login) {
			t.Fatalf("login: code %d out %q err %q", code, out, errOut)
		}
	}
	return reinHome
}

// stubJudgeAnswer is the judge answer the stub claude streams.
const stubJudgeAnswer = `{"version":2,"candidates":[{"candidate":"rules/go","verdict":"new","suggestion":"keep","reason":"fine"},{"candidate":"rules/instructions","verdict":"new","suggestion":"keep","reason":"fine"}]}`

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
	folder := filepath.Join(root, "declarations")
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
		if code != cli.ExitWorked || !strings.Contains(out, "  login        Danidani23 · agents claude-code") {
			t.Fatalf("code %d out %q", code, out)
		}
	})
	t.Run("a machine without a login refuses org list and is shown by status", func(t *testing.T) {
		isolate(t, "")
		code, _, errOut := run("org", "list")
		if code != cli.ExitRefused || !strings.Contains(errOut, "rein login") {
			t.Fatalf("code %d err %q", code, errOut)
		}
		code, out, _ := run("status")
		if code != cli.ExitWorked || !strings.Contains(out, "  login        none; run rein login") {
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
		code, out, _ = run("reset", "machine", "--yes")
		if code != cli.ExitWorked || !strings.Contains(out, "removed  clone") {
			t.Fatalf("reset: code %d out %q", code, out)
		}
		_, err = os.Stat(filepath.Join(reinHome, "orgs"))
		if err == nil {
			t.Fatalf("orgs folder still there")
		}
	})
	t.Run("org create runs through the fake GitHub and real git, and snapshots the agent home", func(t *testing.T) {
		reinHome := isolate(t, "Danidani23")
		claudeHome := os.Getenv("CLAUDE_CONFIG_DIR")
		_ = os.MkdirAll(claudeHome, 0o755)
		_ = os.WriteFile(filepath.Join(claudeHome, "CLAUDE.md"), []byte("be brief"), 0o644)
		code, out, errOut := run("org", "create", "--name", "Acme", "--yes")
		remote := filepath.Join(reinHome, "stub-remotes", "acme-agent-settings.git")
		if code != cli.ExitWorked || !strings.Contains(out, "created Danidani23/acme-agent-settings (private) at "+remote) || !strings.Contains(out, "protected main") || !strings.Contains(out, "connected acme (Acme) at") || !strings.Contains(out, "snapshot of an agent's local settings, kept on this machine: "+filepath.Join(reinHome, "snapshots")) {
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
		code, out, errOut = run("project", "create", "--name", "Shop", "--team", "web", "--label", "kind:service", "--label", "language:go", "--yes")
		if code != cli.ExitWorked || !strings.Contains(out, "created project shop (Shop) in acme") {
			t.Fatalf("project: code %d out %q err %q", code, out, errOut)
		}
		code, out, errOut = run("resolve", "--project", "shop")
		if code != cli.ExitWorked || !strings.Contains(out, "labels kind:service, language:go, user danidani23") || !strings.Contains(out, "no declarations apply yet") {
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
		arguments := filepath.Join(t.TempDir(), "claude-arguments")
		t.Setenv("REIN_TEST_CLAUDE_ARGUMENTS", arguments)
		code, out, errOut = run("distill", "--path", project, "--model", "sonnet")
		passed, _ := os.ReadFile(arguments)
		if !strings.Contains(string(passed), "--model sonnet") {
			t.Fatalf("claude was started with %q, without the model", passed)
		}
		if code != cli.ExitWorked || !strings.Contains(out, "distilled 2 candidates for organizations.acme.projects.shop") || !strings.Contains(out, "rules/go") || !strings.Contains(out, "written to "+filepath.Join(project, ".rein", "proposals", "proposal-")) || !strings.Contains(out, "distiller: agent-cli:claude answered\n") || !strings.Contains(out, "the distiller's conversation, to read or go on with it: claude --resume ") {
			t.Fatalf("distill: code %d out %q err %q", code, out, errOut)
		}
		ignore, _ = os.ReadFile(filepath.Join(project, ".gitignore"))
		if !strings.Contains(string(ignore), ".rein/") {
			t.Fatalf("the rein folder is not ignored: %q", ignore)
		}
		distilled := ""
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(line, "written to ") {
				distilled = strings.TrimPrefix(line, "written to ")
			}
		}
		code, out, errOut = run("propose", distilled, "--yes")
		if code != cli.ExitWorked || !strings.Contains(out, "proposed 2 declarations on branch rein/propose-shop-") || !strings.Contains(out, "pull request: https://github.com/Danidani23/acme-agent-settings/pull/1") || !strings.Contains(out, "the proposal moved to "+filepath.Join(reinHome, "proposals", "acme")) {
			t.Fatalf("propose: code %d out %q err %q", code, out, errOut)
		}
		_, err = os.Stat(distilled)
		if err == nil {
			t.Fatalf("the pending proposal stayed after propose")
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
		// reset project puts the project back and commits; the untracked local file returns untracked
		code, out, errOut = run("reset", "project", "--path", project, "--yes")
		if code != cli.ExitWorked || !strings.Contains(out, "removed sync's files: .claude/settings.local.json, .claude/skills/rein/SKILL.md, .mcp.json, CLAUDE.md") || !strings.Contains(out, "put back: .claude/rules/go.md, CLAUDE.md") || !strings.Contains(out, ".gitignore: rein block removed") || !strings.Contains(out, "committed ") {
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
		// destroy disconnects and prints the deletion steps; the stub remote stays
		code, out, errOut = run("org", "destroy", "acme", "--yes")
		if code != cli.ExitWorked || !strings.Contains(out, "disconnected acme: removed the clone at") || !strings.Contains(out, "1. Open "+os.Getenv("REIN_GITHUB_WEB")+"/Danidani23/acme-agent-settings/settings") || !strings.Contains(out, "Danger Zone") {
			t.Fatalf("destroy: code %d out %q err %q", code, out, errOut)
		}
		_, err = os.Stat(remote)
		if err != nil {
			t.Fatalf("the remote is gone after destroy: %v", err)
		}
		// reset machine wipes the rest
		code, out, errOut = run("reset", "machine", "--yes")
		if code != cli.ExitWorked || !strings.Contains(out, "removals.yaml") {
			t.Fatalf("reset: code %d out %q err %q", code, out, errOut)
		}
		for _, gone := range []string{"orgs", "snapshots", "proposals", "removals.yaml", "inserts"} {
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
		if code != cli.ExitWorked || !strings.Contains(errOut, "warning: main is not protected; GitHub said: Upgrade to GitHub Pro") || !strings.Contains(out, "connected acme (Acme)") {
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

// TestMCPOverStandardStreams starts the MCP gateway the way an agent does,
// through the command over stdin and stdout, opens with the handshake every
// client still speaks, lists the tools and calls one, and ends by closing
// the input. Nothing but protocol messages may reach stdout.
func TestMCPOverStandardStreams(t *testing.T) {
	isolate(t, "")
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()
	done := make(chan int, 1)
	go func() {
		var errOut bytes.Buffer
		done <- cli.Run([]string{"mcp"}, stdinReader, stdoutWriter, &errOut, wire)
		_ = stdoutWriter.Close()
	}()
	scanner := bufio.NewScanner(stdoutReader)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	exchange := func(line string) map[string]any {
		t.Helper()
		_, err := io.WriteString(stdinWriter, line+"\n")
		if err != nil {
			t.Fatalf("stdin: %v", err)
		}
		if !scanner.Scan() {
			t.Fatalf("no answer to %s", line)
		}
		var message map[string]any
		err = json.Unmarshal(scanner.Bytes(), &message)
		if err != nil {
			t.Fatalf("stdout carries something that is not a message: %q", scanner.Text())
		}
		return message
	}
	opened := exchange(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`)
	encoded, _ := json.Marshal(opened)
	if !strings.Contains(string(encoded), `"protocolVersion":"2025-11-25"`) || !strings.Contains(string(encoded), `"name":"rein"`) {
		t.Fatalf("initialize answered %s", encoded)
	}
	_, _ = io.WriteString(stdinWriter, `{"jsonrpc":"2.0","method":"notifications/initialized"}`+"\n")
	listed, _ := json.Marshal(exchange(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`))
	for _, tool := range []string{"status", "resolve", "lint"} {
		if !strings.Contains(string(listed), `"name":"`+tool+`"`) {
			t.Fatalf("tools/list lacks %s: %s", tool, listed)
		}
	}
	called, _ := json.Marshal(exchange(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"status","arguments":{}}}`))
	if !strings.Contains(string(called), `"structuredContent"`) || strings.Contains(string(called), `"isError":true`) {
		t.Fatalf("status answered %s", called)
	}
	_ = stdinWriter.Close()
	if code := <-done; code != cli.ExitWorked {
		t.Fatalf("mcp ended with %d", code)
	}
}
