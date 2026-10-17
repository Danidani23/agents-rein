package cli

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
	"github.com/Danidani23/agents-rein/internal/ports"
)

func TestOutputGoesThroughTheWriters(t *testing.T) {
	forbidden := []string{"fmt.Print", "os.Stdout", "os.Stderr"}
	for _, directory := range []string{filepath.Join("..", "..", "..", "..", "cmd"), filepath.Join("..", "..", "..")} {
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

// The fake orchestrator: canned results and a record of the calls, so the
// gateway's own behaviour, printing, exit codes and confirmations, is tested
// without any adapter.

type fakeWorkflows struct {
	connect    orchestration.ConnectResult
	disconnect orchestration.DisconnectResult
	reset      orchestration.ResetResult
	err        error
	calls      []string
	applied    bool
}

func (fake *fakeWorkflows) ConnectOrganization(remote string) (orchestration.ConnectResult, error) {
	fake.calls = append(fake.calls, "connect "+remote)
	return fake.connect, fake.err
}

func (fake *fakeWorkflows) DisconnectOrganization(slug string) (orchestration.DisconnectResult, error) {
	fake.calls = append(fake.calls, "disconnect "+slug)
	return fake.disconnect, fake.err
}

func (fake *fakeWorkflows) ResetMachine(plan orchestration.ResetPlan) (orchestration.ResetResult, error) {
	fake.applied = true
	return fake.reset, fake.err
}

type fakeQueries struct {
	status orchestration.StatusReport
	plan   orchestration.ResetPlan
	err    error
}

func (fake *fakeQueries) Status() (orchestration.StatusReport, error) {
	return fake.status, fake.err
}

func (fake *fakeQueries) PlanReset(all bool) (orchestration.ResetPlan, error) {
	fake.plan.All = all
	return fake.plan, fake.err
}

type fakeOrganizations struct {
	list   []machine.OrganizationRef
	active string
	err    error
	calls  []string
}

func (fake *fakeOrganizations) List() ([]machine.OrganizationRef, string, error) {
	fake.calls = append(fake.calls, "list")
	return fake.list, fake.active, fake.err
}

func (fake *fakeOrganizations) Use(slug string) error {
	fake.calls = append(fake.calls, "use "+slug)
	return fake.err
}

type harness struct {
	workflows     *fakeWorkflows
	queries       *fakeQueries
	organizations *fakeOrganizations
}

func newHarness() harness {
	return harness{workflows: &fakeWorkflows{}, queries: &fakeQueries{}, organizations: &fakeOrganizations{}}
}

func (fixture harness) orchestrate() (orchestration.Orchestrator, error) {
	return orchestration.Orchestrator{Workflows: fixture.workflows, Queries: fixture.queries, Organizations: fixture.organizations}, nil
}

// run executes rein with empty stdin, so every command sees no terminal.
func (fixture harness) run(arguments ...string) (code int, out string, errOut string) {
	var outBuffer, errBuffer bytes.Buffer
	code = Run(arguments, strings.NewReader(""), &outBuffer, &errBuffer, fixture.orchestrate)
	return code, outBuffer.String(), errBuffer.String()
}

// runInteractive executes rein as if a person typed the answers.
func (fixture harness) runInteractive(answers string, arguments ...string) (code int, out string, errOut string) {
	var outBuffer, errBuffer bytes.Buffer
	env := &environment{in: strings.NewReader(answers), out: &outBuffer, errOut: &errBuffer, terminal: true, orchestrate: fixture.orchestrate}
	code = runCommand(env, arguments)
	return code, outBuffer.String(), errBuffer.String()
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

func TestExitCodes(t *testing.T) {
	t.Run("a refusal from the orchestrator exits 1", func(t *testing.T) {
		fixture := newHarness()
		fixture.organizations.err = orchestration.Refusal{Reason: "gh is not logged in"}
		code, _, errOut := fixture.run("org", "list")
		if code != ExitRefused || !strings.Contains(errOut, "rein: gh is not logged in") {
			t.Fatalf("code %d err %q", code, errOut)
		}
	})
	t.Run("an unexpected error exits 2", func(t *testing.T) {
		fixture := newHarness()
		fixture.organizations.err = errors.New("disk on fire")
		code, _, _ := fixture.run("org", "list")
		if code != ExitCrashed {
			t.Fatalf("code %d", code)
		}
	})
	t.Run("an unknown command and a missing argument exit 1", func(t *testing.T) {
		fixture := newHarness()
		for _, arguments := range [][]string{{"frobnicate"}, {"org", "connect"}, {"lint"}} {
			code, _, _ := fixture.run(arguments...)
			if code != ExitRefused {
				t.Fatalf("%v: code %d", arguments, code)
			}
		}
	})
	t.Run("help and version never touch the orchestrator", func(t *testing.T) {
		var outBuffer, errBuffer bytes.Buffer
		touched := false
		orchestrate := func() (orchestration.Orchestrator, error) {
			touched = true
			return orchestration.Orchestrator{}, nil
		}
		Run([]string{"--help"}, strings.NewReader(""), &outBuffer, &errBuffer, orchestrate)
		Run([]string{"version"}, strings.NewReader(""), &outBuffer, &errBuffer, orchestrate)
		if touched || !strings.Contains(outBuffer.String(), "rein ") {
			t.Fatalf("touched %v out %q", touched, outBuffer.String())
		}
	})
}

func TestLint(t *testing.T) {
	t.Run("a clean tree exits 0 and a tree with an error exits 1", func(t *testing.T) {
		fixture := newHarness()
		root := t.TempDir()
		writeFile(t, root, "skills/commit/SKILL.md", "---\nname: commit\ndescription: Draft commits.\n---\n")
		code, out, _ := fixture.run("lint", root)
		if code != ExitWorked || !strings.Contains(out, "0 errors, 0 warnings") {
			t.Fatalf("code %d out %q", code, out)
		}
		writeFile(t, root, "skills/commit/SKILL.md", "---\nname: commits\ndescription: Draft commits.\n---\n")
		code, out, errOut := fixture.run("lint", root)
		if code != ExitRefused || !strings.Contains(out, "SKILL.md: error:") || !strings.Contains(errOut, "lint found 1 errors") {
			t.Fatalf("code %d out %q err %q", code, out, errOut)
		}
	})
	t.Run("a missing path exits 2", func(t *testing.T) {
		code, _, _ := newHarness().run("lint", filepath.Join(t.TempDir(), "missing"))
		if code != ExitCrashed {
			t.Fatalf("code %d", code)
		}
	})
}

func TestOrg(t *testing.T) {
	t.Run("connect prints the result and the active organisation", func(t *testing.T) {
		fixture := newHarness()
		fixture.workflows.connect = orchestration.ConnectResult{Slug: "acme", Name: "Acme", Clone: "/clones/1", BecameActive: true}
		code, out, _ := fixture.run("org", "connect", "git@example:acme.git")
		if code != ExitWorked || !strings.Contains(out, "connected acme (Acme) at /clones/1") || !strings.Contains(out, "active organisation: acme") {
			t.Fatalf("code %d out %q", code, out)
		}
		if fixture.workflows.calls[0] != "connect git@example:acme.git" {
			t.Fatalf("calls %v", fixture.workflows.calls)
		}
	})
	t.Run("list marks the active organisation and says when there is none", func(t *testing.T) {
		fixture := newHarness()
		code, out, _ := fixture.run("org", "list")
		if code != ExitWorked || !strings.Contains(out, "no organisations connected") {
			t.Fatalf("code %d out %q", code, out)
		}
		fixture.organizations.list = []machine.OrganizationRef{{Slug: "acme", Remote: "r1"}, {Slug: "other", Remote: "r2"}}
		fixture.organizations.active = "other"
		_, out, _ = fixture.run("org", "list")
		if !strings.Contains(out, "  acme  r1") || !strings.Contains(out, "* other  r2") {
			t.Fatalf("out %q", out)
		}
	})
	t.Run("use and disconnect print their outcomes", func(t *testing.T) {
		fixture := newHarness()
		code, out, _ := fixture.run("org", "use", "acme")
		if code != ExitWorked || !strings.Contains(out, "active organisation: acme") {
			t.Fatalf("code %d out %q", code, out)
		}
		fixture.workflows.disconnect = orchestration.DisconnectResult{Clone: "/clones/1", RemovedProjects: 2}
		code, out, _ = fixture.run("org", "disconnect", "acme")
		if code != ExitWorked || !strings.Contains(out, "removed the clone at /clones/1 and 2 project registrations") || !strings.Contains(out, "no active organisation") {
			t.Fatalf("code %d out %q", code, out)
		}
	})
}

func TestStatus(t *testing.T) {
	t.Run("the report is printed, missing targets flagged", func(t *testing.T) {
		fixture := newHarness()
		fixture.queries.status = orchestration.StatusReport{
			Identity:      ports.Identity{Login: "Danidani23", Slug: "danidani23"},
			Agents:        []string{"claude-code"},
			Home:          "/home/.rein",
			Placements:    []orchestration.PlacementStatus{{Placement: machine.Placement{Path: "/a", Target: "/t", Mode: "link", Owner: "import"}, TargetExists: false}},
			Missing:       1,
			Organizations: []machine.OrganizationRef{{Slug: "acme", Remote: "r"}},
			Active:        "acme",
		}
		code, out, _ := fixture.run("status")
		for _, expected := range []string{"github      Danidani23  (active gh account; slug danidani23)", "agents      claude-code", "placements  1", "missing  import", "1 targets are missing", "* acme  r"} {
			if code != ExitWorked || !strings.Contains(out, expected) {
				t.Fatalf("missing %q: code %d out %q", expected, code, out)
			}
		}
	})
	t.Run("a logged-out identity is shown, not refused", func(t *testing.T) {
		fixture := newHarness()
		fixture.queries.status = orchestration.StatusReport{IdentityError: "gh is not logged in"}
		code, out, _ := fixture.run("status")
		if code != ExitWorked || !strings.Contains(out, "github      none: gh is not logged in") || !strings.Contains(out, "agents      none found") {
			t.Fatalf("code %d out %q", code, out)
		}
	})
}

func TestReset(t *testing.T) {
	plan := orchestration.ResetPlan{Placements: []machine.Placement{{Path: "/home/.claude/rules/global.md"}}, ImportsFolder: "/home/.rein/imports"}
	t.Run("the summary lists the paths, ? explains, and a yes applies", func(t *testing.T) {
		fixture := newHarness()
		fixture.queries.plan = plan
		fixture.workflows.reset = orchestration.ResetResult{Placements: []orchestration.Unplaced{{Placement: plan.Placements[0], Removed: true}}}
		code, out, _ := fixture.runInteractive("?\n\n", "reset")
		if code != ExitWorked || !strings.Contains(out, "/home/.claude/rules/global.md") || !strings.Contains(out, "A placement is one link") || !strings.Contains(out, "removed  /home/.claude/rules/global.md") || !fixture.workflows.applied {
			t.Fatalf("code %d out %q applied %v", code, out, fixture.workflows.applied)
		}
	})
	t.Run("a no changes nothing", func(t *testing.T) {
		fixture := newHarness()
		fixture.queries.plan = plan
		code, _, errOut := fixture.runInteractive("n\n", "reset")
		if code != ExitRefused || !strings.Contains(errOut, "nothing changed") || fixture.workflows.applied {
			t.Fatalf("code %d err %q applied %v", code, errOut, fixture.workflows.applied)
		}
	})
	t.Run("without a terminal and without --yes it refuses", func(t *testing.T) {
		fixture := newHarness()
		code, _, errOut := fixture.run("reset")
		if code != ExitRefused || !strings.Contains(errOut, "--yes") || fixture.workflows.applied {
			t.Fatalf("code %d err %q", code, errOut)
		}
	})
	t.Run("a skipped placement is reported and exits 1", func(t *testing.T) {
		fixture := newHarness()
		fixture.queries.plan = plan
		fixture.workflows.reset = orchestration.ResetResult{Placements: []orchestration.Unplaced{{Placement: plan.Placements[0], Reason: "links elsewhere"}}, Kept: 1}
		code, _, errOut := fixture.run("reset", "--yes")
		if code != ExitRefused || !strings.Contains(errOut, "skipped  /home/.claude/rules/global.md: links elsewhere") {
			t.Fatalf("code %d err %q", code, errOut)
		}
	})
	t.Run("--all names the organisations and projects", func(t *testing.T) {
		fixture := newHarness()
		fixture.queries.plan = orchestration.ResetPlan{Organizations: []machine.OrganizationRef{{Slug: "acme"}}, OrgsFolder: "/home/.rein/orgs"}
		fixture.workflows.reset = orchestration.ResetResult{RemovedClones: []string{"id-1"}}
		code, out, _ := fixture.run("reset", "--all", "--yes")
		if code != ExitWorked || !strings.Contains(out, "1 organisations with their clones under /home/.rein/orgs") || !strings.Contains(out, "removed  clone id-1") {
			t.Fatalf("code %d out %q", code, out)
		}
	})
}
