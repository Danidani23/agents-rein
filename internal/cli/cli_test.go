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

// isolate points the rein home and the Claude home at temp folders.
func isolate(t *testing.T) (reinHome string, claudeHome string) {
	t.Helper()
	reinHome = t.TempDir()
	claudeHome = filepath.Join(t.TempDir(), ".claude")
	t.Setenv("REIN_HOME", reinHome)
	t.Setenv("CLAUDE_CONFIG_DIR", claudeHome)
	return reinHome, claudeHome
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
	code, _, errOut := run("config", "--user", "danielmolnar", "--user-level", source, "--yes")
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
		if code != ExitWorked || !strings.HasPrefix(out, "rein ") {
			t.Fatalf("code %d out %q", code, out)
		}
	})
}

func TestConfig(t *testing.T) {
	t.Run("flags configure the machine and place the user level", func(t *testing.T) {
		reinHome, claudeHome := isolate(t)
		source := userLevelSource(t)
		code, out, errOut := run("config", "--user", "danielmolnar", "--user-level", source, "--yes")
		if code != ExitWorked {
			t.Fatalf("code %d out %q err %q", code, out, errOut)
		}
		if !strings.Contains(out, "linked") || !strings.Contains(out, "configured "+reinHome) {
			t.Fatalf("out %q", out)
		}
		_, err := os.Stat(filepath.Join(reinHome, "config.yaml"))
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
		code, out, _ = run("config", "--user", "danielmolnar", "--user-level", source, "--yes")
		if code != ExitWorked || !strings.Contains(out, "ok") {
			t.Fatalf("second run: code %d out %q", code, out)
		}
	})
	t.Run("in a terminal it asks every question, explains on ?, and confirms", func(t *testing.T) {
		reinHome, _ := isolate(t)
		source := userLevelSource(t)
		answers := "?\nDaniel\ndanielmolnar\n\n" + source + "\n\n"
		code, out, errOut := runInteractive(answers, "config")
		if code != ExitWorked {
			t.Fatalf("code %d out %q err %q", code, out, errOut)
		}
		for _, expected := range []string{"rein config sets up this machine", "Enter keeps the value in brackets", "The slug names the folder", "lowercase letters", "1) claude-code", "not yet supported", "Continue?", "configured " + reinHome} {
			if !strings.Contains(out, expected) {
				t.Fatalf("missing %q in %q", expected, out)
			}
		}
	})
	t.Run("answering no at the confirmation changes nothing", func(t *testing.T) {
		reinHome, _ := isolate(t)
		code, _, errOut := runInteractive("danielmolnar\n\n"+userLevelSource(t)+"\nn\n", "config")
		if code != ExitRefused || !strings.Contains(errOut, "nothing changed") {
			t.Fatalf("code %d err %q", code, errOut)
		}
		_, err := os.Stat(filepath.Join(reinHome, "config.yaml"))
		if err == nil {
			t.Fatalf("config.yaml was written")
		}
	})
	t.Run("flags without a terminal need --yes", func(t *testing.T) {
		isolate(t)
		code, _, errOut := run("config", "--user", "danielmolnar", "--user-level", userLevelSource(t))
		if code != ExitRefused || !strings.Contains(errOut, "--yes") {
			t.Fatalf("code %d err %q", code, errOut)
		}
	})
	t.Run("a foreign file in the Claude home is refused and reported", func(t *testing.T) {
		_, claudeHome := isolate(t)
		writeFile(t, claudeHome, "CLAUDE.md", "mine\n")
		code, _, errOut := run("config", "--user", "danielmolnar", "--user-level", userLevelSource(t), "--yes")
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
		code, _, errOut := run("config", "--user", "Daniel", "--user-level", userLevelSource(t), "--yes")
		if code != ExitRefused || !strings.Contains(errOut, "slug") {
			t.Fatalf("code %d err %q", code, errOut)
		}
	})
	t.Run("a source without skills is refused", func(t *testing.T) {
		isolate(t)
		code, _, errOut := run("config", "--user", "danielmolnar", "--user-level", t.TempDir(), "--yes")
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
			if code != ExitRefused || !strings.Contains(errOut, "run rein config first") {
				t.Fatalf("%v: code %d err %q", arguments, code, errOut)
			}
		}
	})
	t.Run("connect clones the repository, remembers it and makes it active", func(t *testing.T) {
		reinHome, _ := isolate(t)
		configure(t, userLevelSource(t))
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
		entries, _ := os.ReadDir(filepath.Join(reinHome, "orgs"))
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
		reinHome, _ := isolate(t)
		configure(t, userLevelSource(t))
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

func TestExportAndImport(t *testing.T) {
	t.Run("both refuse before config", func(t *testing.T) {
		isolate(t)
		for _, arguments := range [][]string{{"export"}, {"import", "x.zip"}} {
			code, _, errOut := run(arguments...)
			if code != ExitRefused || !strings.Contains(errOut, "run rein config first") {
				t.Fatalf("%v: code %d err %q", arguments, code, errOut)
			}
		}
	})
	t.Run("export writes the default name in the working directory", func(t *testing.T) {
		isolate(t)
		configure(t, userLevelSource(t))
		workingDirectory := t.TempDir()
		t.Chdir(workingDirectory)
		code, out, errOut := run("export")
		if code != ExitWorked || !strings.Contains(out, "1 rules, 1 skills") {
			t.Fatalf("code %d out %q err %q", code, out, errOut)
		}
		matches, _ := filepath.Glob(filepath.Join(workingDirectory, "rein-export-danielmolnar-*.zip"))
		if len(matches) != 1 {
			t.Fatalf("zip not written: %v", matches)
		}
	})
	t.Run("export to a path, then import places rules and skills and records them", func(t *testing.T) {
		reinHome, claudeHome := isolate(t)
		configure(t, userLevelSource(t))
		zipPath := filepath.Join(t.TempDir(), "level.zip")
		code, _, errOut := run("export", "--out", zipPath)
		if code != ExitWorked {
			t.Fatalf("export: code %d err %q", code, errOut)
		}
		// the receiver: a second machine with its own homes and a plain config
		receiverRein := t.TempDir()
		receiverClaude := filepath.Join(t.TempDir(), ".claude")
		t.Setenv("REIN_HOME", receiverRein)
		t.Setenv("CLAUDE_CONFIG_DIR", receiverClaude)
		configure(t, userLevelSource(t))
		_ = reinHome
		_ = claudeHome
		code, out, errOut := run("import", zipPath)
		if code != ExitRefused || !strings.Contains(out, "imported ") {
			t.Fatalf("import: code %d out %q err %q", code, out, errOut)
		}
		resolved, err := os.Readlink(filepath.Join(receiverClaude, "rules", "global.md"))
		if err != nil || !strings.HasPrefix(resolved, filepath.Join(receiverRein, "imports")) {
			t.Fatalf("rule link %q err %v", resolved, err)
		}
		state, _ := os.ReadFile(filepath.Join(receiverRein, "state.yaml"))
		if !strings.Contains(string(state), "imports:") || !strings.Contains(string(state), "rules/global.md") {
			t.Fatalf("state %s", state)
		}
		// the receiver's own config already placed skills/commit, so the same skill id is refused
		if !strings.Contains(errOut, "refused") || !strings.Contains(errOut, "skills/commit") || !strings.Contains(errOut, "1 targets refused") {
			t.Fatalf("expected the same-named skill to be refused: out %q err %q", out, errOut)
		}
		resolved, err = os.Readlink(filepath.Join(receiverClaude, "skills", "commit"))
		if err != nil || strings.Contains(resolved, "imports") {
			t.Fatalf("config's skill link was replaced: %q err %v", resolved, err)
		}
		code, out, _ = run("import", zipPath)
		if code != ExitRefused || !strings.Contains(out, "ok ") {
			t.Fatalf("second import: code %d out %q", code, out)
		}
	})
	t.Run("a zip that is not a rein export is refused", func(t *testing.T) {
		isolate(t)
		configure(t, userLevelSource(t))
		plain := filepath.Join(t.TempDir(), "plain.zip")
		writeFile(t, filepath.Dir(plain), "plain.zip", "not a zip")
		code, _, errOut := run("import", plain)
		if code != ExitRefused || !strings.Contains(errOut, "not a zip") {
			t.Fatalf("code %d err %q", code, errOut)
		}
	})
	t.Run("an export with a lint error is refused and nothing is placed", func(t *testing.T) {
		_, claudeHome := isolate(t)
		bad := t.TempDir()
		writeFile(t, bad, "global/AGENTS.md", "# Global\n")
		writeFile(t, bad, "skills/commit/SKILL.md", "---\nname: commits\ndescription: d\n---\n")
		configure(t, bad)
		zipPath := filepath.Join(t.TempDir(), "bad.zip")
		run("export", "--out", zipPath)
		receiver := t.TempDir()
		t.Setenv("REIN_HOME", receiver)
		t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(t.TempDir(), ".claude"))
		configure(t, userLevelSource(t))
		code, _, errOut := run("import", zipPath)
		if code != ExitRefused || !strings.Contains(errOut, "lint errors") {
			t.Fatalf("code %d err %q", code, errOut)
		}
		_ = claudeHome
	})
	t.Run("an unsupported agent in config is refused", func(t *testing.T) {
		isolate(t)
		code, _, errOut := run("config", "--user", "danielmolnar", "--user-level", userLevelSource(t), "--agent", "codex", "--yes")
		if code != ExitRefused || !strings.Contains(errOut, "not supported") {
			t.Fatalf("code %d err %q", code, errOut)
		}
	})
}

func TestStatus(t *testing.T) {
	t.Run("before config it says so and exits 0", func(t *testing.T) {
		isolate(t)
		code, out, _ := run("status")
		if code != ExitWorked || !strings.Contains(out, "not configured") {
			t.Fatalf("code %d out %q", code, out)
		}
	})
	t.Run("after config it lists the placements and flags a missing target", func(t *testing.T) {
		_, _ = isolate(t)
		source := userLevelSource(t)
		configure(t, source)
		code, out, _ := run("status")
		if code != ExitWorked || !strings.Contains(out, "user        danielmolnar") || !strings.Contains(out, "placements  2") || !strings.Contains(out, "ok       config") {
			t.Fatalf("code %d out %q", code, out)
		}
		err := os.RemoveAll(filepath.Join(source, "skills", "commit"))
		if err != nil {
			t.Fatalf("remove: %v", err)
		}
		code, out, _ = run("status")
		if code != ExitWorked || !strings.Contains(out, "missing") || !strings.Contains(out, "1 targets are missing") {
			t.Fatalf("code %d out %q", code, out)
		}
	})
}
