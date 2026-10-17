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
	return reinHome
}

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
		code, out, _ = run("reset", "--all", "--yes")
		if code != cli.ExitWorked || !strings.Contains(out, "removed  clone") {
			t.Fatalf("reset: code %d out %q", code, out)
		}
		_, err = os.Stat(filepath.Join(reinHome, "orgs"))
		if err == nil {
			t.Fatalf("orgs folder still there")
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
