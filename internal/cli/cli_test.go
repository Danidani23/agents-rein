package cli

import (
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
			if strings.HasSuffix(filepath.ToSlash(filePath), "cmd/reins/main.go") {
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

// run executes reins with empty stdin, so every command sees no terminal.
func run(arguments ...string) (code int, out string, errOut string) {
	var outBuffer, errBuffer bytes.Buffer
	code = Run(arguments, strings.NewReader(""), &outBuffer, &errBuffer)
	return code, outBuffer.String(), errBuffer.String()
}

// isolate points the reins home and the Claude home at temp folders.
func isolate(t *testing.T) (reinsHome string, claudeHome string) {
	t.Helper()
	reinsHome = t.TempDir()
	claudeHome = filepath.Join(t.TempDir(), ".claude")
	t.Setenv("REINS_HOME", reinsHome)
	t.Setenv("CLAUDE_CONFIG_DIR", claudeHome)
	return reinsHome, claudeHome
}

func userLevelSource(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "global/AGENTS.md", "# Global\n")
	writeFile(t, root, "skills/commit/SKILL.md", "---\nname: commit\ndescription: Draft commits.\n---\n")
	return root
}

func configure(t *testing.T, source string) {
	t.Helper()
	code, _, errOut := run("config", "--user", "danielmolnar", "--user-level", source)
	if code != ExitWorked {
		t.Fatalf("config: code %d err %q", code, errOut)
	}
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
	t.Run("lint needs no config", func(t *testing.T) {
		isolate(t)
		root := t.TempDir()
		code, _, _ := run("lint", root)
		if code != ExitWorked {
			t.Fatalf("code %d", code)
		}
	})
}

func TestVersion(t *testing.T) {
	t.Run("version prints without config", func(t *testing.T) {
		isolate(t)
		code, out, _ := run("version")
		if code != ExitWorked || !strings.HasPrefix(out, "reins ") {
			t.Fatalf("code %d out %q", code, out)
		}
	})
}

func TestConfig(t *testing.T) {
	t.Run("flags configure the machine and place the user level", func(t *testing.T) {
		reinsHome, claudeHome := isolate(t)
		source := userLevelSource(t)
		code, out, errOut := run("config", "--user", "danielmolnar", "--user-level", source)
		if code != ExitWorked {
			t.Fatalf("code %d out %q err %q", code, out, errOut)
		}
		if !strings.Contains(out, "linked") || !strings.Contains(out, "configured "+reinsHome) {
			t.Fatalf("out %q", out)
		}
		_, err := os.Stat(filepath.Join(reinsHome, "config.yaml"))
		if err != nil {
			t.Fatalf("config.yaml: %v", err)
		}
		resolved, err := os.Readlink(filepath.Join(claudeHome, "CLAUDE.md"))
		if err != nil || resolved != filepath.Join(source, "global", "AGENTS.md") {
			t.Fatalf("CLAUDE.md link %q err %v", resolved, err)
		}
		_, err = os.Readlink(filepath.Join(claudeHome, "skills", "commit"))
		if err != nil {
			t.Fatalf("skill link: %v", err)
		}
		code, out, _ = run("config", "--user", "danielmolnar", "--user-level", source)
		if code != ExitWorked || !strings.Contains(out, "ok") {
			t.Fatalf("second run: code %d out %q", code, out)
		}
	})
	t.Run("a foreign file in the Claude home is refused and reported", func(t *testing.T) {
		_, claudeHome := isolate(t)
		writeFile(t, claudeHome, "CLAUDE.md", "mine\n")
		code, _, errOut := run("config", "--user", "danielmolnar", "--user-level", userLevelSource(t))
		if code != ExitRefused || !strings.Contains(errOut, "refused") {
			t.Fatalf("code %d err %q", code, errOut)
		}
	})
	t.Run("without a terminal and without flags it refuses and names the flags", func(t *testing.T) {
		isolate(t)
		code, _, errOut := run("config")
		if code != ExitRefused || !strings.Contains(errOut, "--user") {
			t.Fatalf("code %d err %q", code, errOut)
		}
	})
	t.Run("a slug with uppercase is refused", func(t *testing.T) {
		isolate(t)
		code, _, errOut := run("config", "--user", "Daniel", "--user-level", userLevelSource(t))
		if code != ExitRefused || !strings.Contains(errOut, "slug") {
			t.Fatalf("code %d err %q", code, errOut)
		}
	})
	t.Run("a source without skills is refused", func(t *testing.T) {
		isolate(t)
		code, _, errOut := run("config", "--user", "danielmolnar", "--user-level", t.TempDir())
		if code != ExitRefused || !strings.Contains(errOut, "not a user-level source") {
			t.Fatalf("code %d err %q", code, errOut)
		}
	})
}

func TestOrg(t *testing.T) {
	t.Run("every org command refuses before config", func(t *testing.T) {
		isolate(t)
		for _, arguments := range [][]string{{"org", "list"}, {"org", "connect", "x"}, {"org", "use", "x"}, {"org", "disconnect", "x"}} {
			code, _, errOut := run(arguments...)
			if code != ExitRefused || !strings.Contains(errOut, "run reins config first") {
				t.Fatalf("%v: code %d err %q", arguments, code, errOut)
			}
		}
	})
	t.Run("connect clones the repository, remembers it and makes it active", func(t *testing.T) {
		reinsHome, _ := isolate(t)
		configure(t, userLevelSource(t))
		remote := scratchOrgRepository(t, "acme")
		code, out, errOut := run("org", "connect", remote)
		if code != ExitWorked || !strings.Contains(out, "connected acme (Acme)") || !strings.Contains(out, "active organisation: acme") {
			t.Fatalf("code %d out %q err %q", code, out, errOut)
		}
		_, err := os.Stat(filepath.Join(reinsHome, "orgs", "0f8fad5b-d9cb-469f-a165-70867728950e", "declarations", "organizations", "acme", "organization.yaml"))
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
		reinsHome, _ := isolate(t)
		configure(t, userLevelSource(t))
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
		entries, _ := os.ReadDir(filepath.Join(reinsHome, "orgs"))
		if len(entries) != 0 {
			t.Fatalf("leftover clones: %v", entries)
		}
	})
	t.Run("a remote that cannot be cloned exits 3", func(t *testing.T) {
		isolate(t)
		configure(t, userLevelSource(t))
		code, _, errOut := run("org", "connect", filepath.Join(t.TempDir(), "nowhere"))
		if code != ExitToolFailed || !strings.Contains(errOut, "git clone") {
			t.Fatalf("code %d err %q", code, errOut)
		}
	})
	t.Run("use switches and refuses an unknown slug", func(t *testing.T) {
		isolate(t)
		configure(t, userLevelSource(t))
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
		reinsHome, _ := isolate(t)
		configure(t, userLevelSource(t))
		run("org", "connect", scratchOrgRepository(t, "acme"))
		code, out, errOut := run("org", "disconnect", "acme")
		if code != ExitWorked || !strings.Contains(out, "disconnected acme") || !strings.Contains(out, "no active organisation") {
			t.Fatalf("code %d out %q err %q", code, out, errOut)
		}
		_, err := os.Stat(filepath.Join(reinsHome, "orgs", "0f8fad5b-d9cb-469f-a165-70867728950e"))
		if !os.IsNotExist(err) {
			t.Fatalf("clone still there: %v", err)
		}
		code, out, _ = run("org", "list")
		if code != ExitWorked || !strings.Contains(out, "no organisations connected") {
			t.Fatalf("list: code %d out %q", code, out)
		}
	})
}
