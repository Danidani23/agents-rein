package cli

import (
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Danidani23/agents-rein/internal/local"
)

func TestOutputGoesThroughTheWriters(t *testing.T) {
	forbidden := []string{"fmt.Print", "os.Stdout", "os.Stderr"}
	for _, directory := range []string{filepath.Join("..", "..", "cmd"), filepath.Join("..")} {
		err := filepath.WalkDir(directory, func(filePath string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(filePath, ".go") || strings.HasSuffix(filePath, "_test.go") {
				return nil
			}
			if strings.HasSuffix(filepath.ToSlash(filePath), "cmd/rein/main.go") {
				return nil
			}
			content, err := os.ReadFile(filePath)
			if err != nil {
				return err
			}
			for _, pattern := range forbidden {
				if strings.Contains(string(content), pattern) {
					t.Errorf("%s uses %s; output goes through the writers Run receives", filePath, pattern)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk: %v", err)
		}
	}
}

func writeFile(t *testing.T, root string, relative string, content string) {
	t.Helper()
	fullPath := filepath.Join(root, relative)
	err := os.MkdirAll(filepath.Dir(fullPath), 0o755)
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	err = os.WriteFile(fullPath, []byte(content), 0o644)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
}

// run executes rein with empty stdin, so every command sees no terminal.
func run(arguments ...string) (code int, out string, errOut string) {
	var outBuffer, errBuffer bytes.Buffer
	code = Run(arguments, strings.NewReader(""), &outBuffer, &errBuffer)
	return code, outBuffer.String(), errBuffer.String()
}

// runInteractive executes rein as if a person typed the answers: the
// environment is built directly with terminal set, since a strings.Reader
// is never a terminal.
func runInteractive(answers string, arguments ...string) (code int, out string, errOut string) {
	var outBuffer, errBuffer bytes.Buffer
	env := &environment{in: strings.NewReader(answers), out: &outBuffer, errOut: &errBuffer, terminal: true}
	root := newRootCommand(env)
	root.SetArgs(arguments)
	err := root.Execute()
	code = ExitWorked
	if err != nil {
		errBuffer.WriteString("rein: " + err.Error() + "\n")
		code = ExitRefused
	}
	return code, outBuffer.String(), errBuffer.String()
}

// isolate points the rein home and the Claude home at temp folders and puts a
// stub gh on the PATH that is logged in as Danidani23.
func isolate(t *testing.T) (reinHome string, claudeHome string) {
	t.Helper()
	reinHome = t.TempDir()
	claudeHome = filepath.Join(t.TempDir(), ".claude")
	t.Setenv("REIN_HOME", reinHome)
	t.Setenv("CLAUDE_CONFIG_DIR", claudeHome)
	stubGitHub(t, "Danidani23")
	return reinHome, claudeHome
}

// stubGitHub puts a fake gh and a fake claude first on the PATH. An empty
// login makes gh behave as logged out.
func stubGitHub(t *testing.T, login string) {
	t.Helper()
	directory := t.TempDir()
	script := "#!/bin/sh\ncase \"$*\" in\n  \"api user --jq .login\")"
	if login == "" {
		script += " echo 'gh: not logged in' >&2; exit 1;;\n"
	} else {
		script += " echo " + login + ";;\n"
	}
	script += "  *) exit 1;;\nesac\n"
	err := os.WriteFile(filepath.Join(directory, "gh"), []byte(script), 0o755)
	if err != nil {
		t.Fatalf("stub gh: %v", err)
	}
	err = os.WriteFile(filepath.Join(directory, "claude"), []byte("#!/bin/sh\nexit 0\n"), 0o755)
	if err != nil {
		t.Fatalf("stub claude: %v", err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func userLevelSource(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "global/AGENTS.md", "# Global\n")
	writeFile(t, root, "skills/commit/SKILL.md", "---\nname: commit\ndescription: Draft commits.\n---\n")
	return root
}

// placeLegacy records two placements the way the retired import did, one of
// them a real link, so status and reset have something to act on.
func placeLegacy(t *testing.T, reinHome string, claudeHome string) (ruleLink string) {
	t.Helper()
	target := filepath.Join(reinHome, "imports", "abc", "rules", "global.md")
	writeFile(t, filepath.Dir(target), "global.md", "# Global\n")
	ruleLink = filepath.Join(claudeHome, "rules", "global.md")
	err := os.MkdirAll(filepath.Dir(ruleLink), 0o755)
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	err = os.Symlink(target, ruleLink)
	if err != nil {
		t.Fatalf("symlink: %v", err)
	}
	state, err := local.LoadState(reinHome)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	state.Placements = []local.Placement{
		{Path: ruleLink, Target: target, Mode: "link", Owner: local.OwnerImport, Source: "/toolkit"},
		{Path: filepath.Join(claudeHome, "skills", "gone"), Target: filepath.Join(reinHome, "imports", "abc", "skills", "gone"), Mode: "link", Owner: local.OwnerImport, Source: "/toolkit"},
	}
	state.Imports = []local.Import{{Hash: "abc000000000", Path: filepath.Join(reinHome, "imports", "abc"), Source: "/toolkit", Imported: "2026-10-16T00:00:00Z"}}
	err = local.SaveState(reinHome, state)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	return ruleLink
}

// scratchOrgRepository builds a git repository holding one organisation
// record, so org connect can clone it without a network.
func scratchOrgRepository(t *testing.T, slug string) string {
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
	writeFile(t, root, "declarations/organizations/"+slug+"/organization.yaml", "id: 0f8fad5b-d9cb-469f-a165-70867728950e\nslug: "+slug+"\nname: Acme\n")
	git("add", "declarations")
	git("-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "organisation record")
	return root
}

func TestLint(t *testing.T) {
	t.Run("a clean tree exits 0 and reports the counts", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "skills/commit/SKILL.md", "---\nname: commit\ndescription: Draft commits.\n---\n")
		code, out, errOut := run("lint", root)
		if code != ExitWorked || !strings.Contains(out, "0 errors, 0 warnings") || errOut != "" {
			t.Fatalf("code %d out %q err %q", code, out, errOut)
		}
	})
	t.Run("a tree with an error exits 1 and names the file", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "skills/commit/SKILL.md", "---\nname: commits\ndescription: Draft commits.\n---\n")
		code, out, errOut := run("lint", root)
		if code != ExitRefused || !strings.Contains(out, "SKILL.md: error:") || !strings.Contains(errOut, "lint found 1 errors") {
			t.Fatalf("code %d out %q err %q", code, out, errOut)
		}
	})
	t.Run("a missing path exits 2", func(t *testing.T) {
		code, _, errOut := run("lint", filepath.Join(t.TempDir(), "missing"))
		if code != ExitCrashed || errOut == "" {
			t.Fatalf("code %d err %q", code, errOut)
		}
	})
	t.Run("lint without a path is refused", func(t *testing.T) {
		code, _, _ := run("lint")
		if code != ExitRefused {
			t.Fatalf("code %d", code)
		}
	})
	t.Run("an unknown command is refused", func(t *testing.T) {
		code, _, _ := run("frobnicate")
		if code != ExitRefused {
			t.Fatalf("code %d", code)
		}
	})
	t.Run("lint needs no identity", func(t *testing.T) {
		isolate(t)
		root := t.TempDir()
		code, _, _ := run("lint", root)
		if code != ExitWorked {
			t.Fatalf("code %d", code)
		}
	})
}

func TestVersion(t *testing.T) {
	t.Run("version prints without identity", func(t *testing.T) {
		isolate(t)
		code, out, _ := run("version")
		if code != ExitWorked || !strings.HasPrefix(out, "rein ") {
			t.Fatalf("code %d out %q", code, out)
		}
	})
}

func TestOrg(t *testing.T) {
	t.Run("every org command refuses without a gh login", func(t *testing.T) {
		isolate(t)
		stubGitHub(t, "")
		for _, arguments := range [][]string{{"org", "list"}, {"org", "connect", "x"}, {"org", "use", "x"}, {"org", "disconnect", "x"}} {
			code, _, errOut := run(arguments...)
			if code != ExitRefused || !strings.Contains(errOut, "gh auth login") {
				t.Fatalf("%v: code %d err %q", arguments, code, errOut)
			}
		}
	})
	t.Run("connect clones the repository, remembers it and makes it active", func(t *testing.T) {
		reinHome, _ := isolate(t)
		remote := scratchOrgRepository(t, "acme")
		code, out, errOut := run("org", "connect", remote)
		if code != ExitWorked || !strings.Contains(out, "connected acme (Acme)") || !strings.Contains(out, "active organisation: acme") {
			t.Fatalf("code %d out %q err %q", code, out, errOut)
		}
		_, err := os.Stat(filepath.Join(reinHome, "orgs", "0f8fad5b-d9cb-469f-a165-70867728950e", "declarations", "organizations", "acme", "organization.yaml"))
		if err != nil {
			t.Fatalf("clone: %v", err)
		}
		code, out, _ = run("org", "list")
		if code != ExitWorked || !strings.Contains(out, "* acme  "+remote) {
			t.Fatalf("list: code %d out %q", code, out)
		}
		code, _, errOut = run("org", "connect", remote)
		if code != ExitRefused || !strings.Contains(errOut, "already connected") {
			t.Fatalf("second connect: code %d err %q", code, errOut)
		}
	})
	t.Run("a repository without an organisation record is refused and nothing is kept", func(t *testing.T) {
		reinHome, _ := isolate(t)
		remote := t.TempDir()
		command := exec.Command("git", "init", "-q")
		command.Dir = remote
		err := command.Run()
		if err != nil {
			t.Fatalf("git init: %v", err)
		}
		code, _, errOut := run("org", "connect", remote)
		if code != ExitRefused || !strings.Contains(errOut, "not an org repository") {
			t.Fatalf("code %d err %q", code, errOut)
		}
		entries, _ := os.ReadDir(filepath.Join(reinHome, "orgs"))
		if len(entries) != 0 {
			t.Fatalf("leftover clones: %v", entries)
		}
	})
	t.Run("a remote that cannot be cloned exits 3", func(t *testing.T) {
		isolate(t)
		code, _, errOut := run("org", "connect", filepath.Join(t.TempDir(), "nowhere"))
		if code != ExitToolFailed || !strings.Contains(errOut, "git clone") {
			t.Fatalf("code %d err %q", code, errOut)
		}
	})
	t.Run("use switches and refuses an unknown slug", func(t *testing.T) {
		isolate(t)
		run("org", "connect", scratchOrgRepository(t, "acme"))
		code, out, _ := run("org", "use", "acme")
		if code != ExitWorked || !strings.Contains(out, "active organisation: acme") {
			t.Fatalf("code %d out %q", code, out)
		}
		code, _, errOut := run("org", "use", "other")
		if code != ExitRefused || !strings.Contains(errOut, "not connected") {
			t.Fatalf("code %d err %q", code, errOut)
		}
	})
	t.Run("disconnect removes the clone and the registration and leaves no active organisation", func(t *testing.T) {
		reinHome, _ := isolate(t)
		run("org", "connect", scratchOrgRepository(t, "acme"))
		code, out, errOut := run("org", "disconnect", "acme")
		if code != ExitWorked || !strings.Contains(out, "disconnected acme") || !strings.Contains(out, "no active organisation") {
			t.Fatalf("code %d out %q err %q", code, out, errOut)
		}
		_, err := os.Stat(filepath.Join(reinHome, "orgs", "0f8fad5b-d9cb-469f-a165-70867728950e"))
		if !os.IsNotExist(err) {
			t.Fatalf("clone still there: %v", err)
		}
		code, out, _ = run("org", "list")
		if code != ExitWorked || !strings.Contains(out, "no organisations connected") {
			t.Fatalf("list: code %d out %q", code, out)
		}
	})
}

func TestStatus(t *testing.T) {
	t.Run("without a gh login it says so and still shows the rest", func(t *testing.T) {
		isolate(t)
		stubGitHub(t, "")
		code, out, _ := run("status")
		if code != ExitWorked || !strings.Contains(out, "github      none") || !strings.Contains(out, "placements  0") {
			t.Fatalf("code %d out %q", code, out)
		}
	})
	t.Run("it shows the identity, the agents, the placements, and flags a missing target", func(t *testing.T) {
		reinHome, claudeHome := isolate(t)
		placeLegacy(t, reinHome, claudeHome)
		code, out, _ := run("status")
		for _, expected := range []string{"github      Danidani23  (active gh account; slug danidani23)", "agents      claude-code", "placements  2", "ok       import", "missing  import", "1 targets are missing"} {
			if code != ExitWorked || !strings.Contains(out, expected) {
				t.Fatalf("missing %q: code %d out %q", expected, code, out)
			}
		}
	})
}

func TestReset(t *testing.T) {
	t.Run("reset removes the placements and imports, dangling ones included, and keeps organisations", func(t *testing.T) {
		reinHome, claudeHome := isolate(t)
		ruleLink := placeLegacy(t, reinHome, claudeHome)
		run("org", "connect", scratchOrgRepository(t, "acme"))
		code, out, errOut := run("reset", "--yes")
		if code != ExitWorked || strings.Count(out, "removed  ") != 1 || !strings.Contains(out, "gone     ") {
			t.Fatalf("code %d out %q err %q", code, out, errOut)
		}
		_, err := os.Lstat(ruleLink)
		if err == nil {
			t.Fatalf("link still there")
		}
		_, err = os.Stat(filepath.Join(reinHome, "imports"))
		if err == nil {
			t.Fatalf("imports folder still there")
		}
		code, out, _ = run("status")
		if !strings.Contains(out, "placements  0") || !strings.Contains(out, "* acme") {
			t.Fatalf("status after reset: %q", out)
		}
	})
	t.Run("the summary lists every path and ? explains", func(t *testing.T) {
		reinHome, claudeHome := isolate(t)
		ruleLink := placeLegacy(t, reinHome, claudeHome)
		code, out, _ := runInteractive("?\nn\n", "reset")
		if code != ExitRefused || !strings.Contains(out, ruleLink) || !strings.Contains(out, "A placement is one link") {
			t.Fatalf("code %d out %q", code, out)
		}
	})
	t.Run("a link that points elsewhere is skipped and reported", func(t *testing.T) {
		reinHome, claudeHome := isolate(t)
		ruleLink := placeLegacy(t, reinHome, claudeHome)
		elsewhere := filepath.Join(t.TempDir(), "other.md")
		writeFile(t, filepath.Dir(elsewhere), "other.md", "x")
		_ = os.Remove(ruleLink)
		err := os.Symlink(elsewhere, ruleLink)
		if err != nil {
			t.Fatalf("symlink: %v", err)
		}
		code, _, errOut := run("reset", "--yes")
		if code != ExitRefused || !strings.Contains(errOut, "skipped") || !strings.Contains(errOut, "not what rein recorded") {
			t.Fatalf("code %d err %q", code, errOut)
		}
		_, err = os.Lstat(ruleLink)
		if err != nil {
			t.Fatalf("foreign link was removed")
		}
	})
	t.Run("reset --all forgets organisations and projects and deletes the clones", func(t *testing.T) {
		reinHome, _ := isolate(t)
		run("org", "connect", scratchOrgRepository(t, "acme"))
		code, out, _ := run("reset", "--all", "--yes")
		if code != ExitWorked || !strings.Contains(out, "removed  clone") {
			t.Fatalf("code %d out %q", code, out)
		}
		_, err := os.Stat(filepath.Join(reinHome, "orgs"))
		if err == nil {
			t.Fatalf("orgs folder still there")
		}
		code, out, _ = run("status")
		if !strings.Contains(out, "organisations  0") {
			t.Fatalf("status after reset --all: %q", out)
		}
	})
	t.Run("without a terminal and without --yes it refuses", func(t *testing.T) {
		isolate(t)
		code, _, errOut := run("reset")
		if code != ExitRefused || !strings.Contains(errOut, "--yes") {
			t.Fatalf("code %d err %q", code, errOut)
		}
	})
}
