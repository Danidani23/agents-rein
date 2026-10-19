package cli

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Danidani23/agents-rein/internal/adapters/fake"
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

// The harness: the real orchestrator over the fake adapters and a scratch
// home, so the gateway's printing, exit codes and confirmations are tested on
// real behaviour without the machine (D18).

const validRecord = "id: 0f8fad5b-d9cb-469f-a165-70867728950e\nslug: acme\nname: Acme\nhost: github\n"

type harness struct {
	home       string
	host       *fake.Host
	repository *fake.Repository
	state      *fake.State
	agent      *fake.Agent
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	return &harness{
		home:       t.TempDir(),
		host:       &fake.Host{Login: "Danidani23"},
		repository: &fake.Repository{Record: validRecord},
		state:      fake.NewState(),
		agent:      &fake.Agent{Present: true, HomePath: t.TempDir()},
	}
}

func (fixture *harness) orchestrate() (orchestration.Orchestrator, error) {
	return orchestration.New(orchestration.NewServices(fixture.home, fixture.host, fixture.repository, fixture.state, []ports.Agent{fixture.agent})), nil
}

// seedConnectedOrganization leaves the state as a connect of acme would, with a clone folder
// and the given number of registered projects, and returns the clone.
func (fixture *harness) seedConnectedOrganization(t *testing.T, projects int) string {
	t.Helper()
	clone := filepath.Join(fixture.home, "orgs", "id-1")
	err := os.MkdirAll(clone, 0o755)
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	fixture.state.Current.Organizations = append(fixture.state.Current.Organizations, machine.OrganizationRef{ID: "id-1", Slug: "acme", Remote: "r1", Host: "github"})
	fixture.state.Current.OrgClonePaths["id-1"] = clone
	fixture.state.Current.Active = "acme"
	for index := 0; index < projects; index++ {
		fixture.state.Current.Projects = append(fixture.state.Current.Projects, machine.Project{Organization: "id-1", ClonePath: fmt.Sprintf("/projects/%d", index)})
	}
	return clone
}

// run executes rein with empty stdin, so every command sees no terminal.
func (fixture *harness) run(arguments ...string) (code int, out string, errOut string) {
	var outBuffer, errBuffer bytes.Buffer
	code = Run(arguments, strings.NewReader(""), &outBuffer, &errBuffer, fixture.orchestrate)
	return code, outBuffer.String(), errBuffer.String()
}

// runInteractive executes rein as if a person typed the answers.
func (fixture *harness) runInteractive(answers string, arguments ...string) (code int, out string, errOut string) {
	var outBuffer, errBuffer bytes.Buffer
	environment := &commandEnvironment{in: strings.NewReader(answers), out: &outBuffer, errOut: &errBuffer, terminal: true, orchestrate: fixture.orchestrate}
	code = runCommand(environment, arguments)
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
	t.Run("a refusal exits 1", func(t *testing.T) {
		fixture := newHarness(t)
		fixture.host = &fake.Host{}
		code, _, errOut := fixture.run("org", "list")
		if code != ExitRefused || !strings.Contains(errOut, "rein: gh is not logged in") {
			t.Fatalf("code %d err %q", code, errOut)
		}
	})
	t.Run("an unexpected error exits 2", func(t *testing.T) {
		fixture := newHarness(t)
		fixture.state.FailLoads = 1
		code, _, errOut := fixture.run("org", "list")
		if code != ExitCrashed || !strings.Contains(errOut, "rein: state:") {
			t.Fatalf("code %d err %q", code, errOut)
		}
	})
	t.Run("an unknown command and a missing argument exit 1", func(t *testing.T) {
		fixture := newHarness(t)
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
		fixture := newHarness(t)
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
		code, _, _ := newHarness(t).run("lint", filepath.Join(t.TempDir(), "missing"))
		if code != ExitCrashed {
			t.Fatalf("code %d", code)
		}
	})
}

func TestOrg(t *testing.T) {
	t.Run("every org command passes the identity gate first", func(t *testing.T) {
		fixture := newHarness(t)
		fixture.host = &fake.Host{}
		for _, arguments := range [][]string{{"org", "connect", "x"}, {"org", "list"}, {"org", "use", "acme"}, {"org", "disconnect", "acme"}} {
			code, _, errOut := fixture.run(arguments...)
			if code != ExitRefused || !strings.Contains(errOut, "gh auth login") {
				t.Fatalf("%v: code %d err %q", arguments, code, errOut)
			}
		}
		if len(fixture.state.Current.Organizations) != 0 {
			t.Fatalf("state changed: %+v", fixture.state.Current)
		}
	})
	t.Run("connect prints the result and the active organisation", func(t *testing.T) {
		fixture := newHarness(t)
		code, out, _ := fixture.run("org", "connect", "git@example:acme.git")
		clone := filepath.Join(fixture.home, "orgs", "0f8fad5b-d9cb-469f-a165-70867728950e")
		if code != ExitWorked || !strings.Contains(out, "connected acme (Acme) at "+clone) || !strings.Contains(out, "active organisation: acme") {
			t.Fatalf("code %d out %q", code, out)
		}
		if fixture.state.Current.Active != "acme" {
			t.Fatalf("state %+v", fixture.state.Current)
		}
	})
	t.Run("list marks the active organisation and says when there is none", func(t *testing.T) {
		fixture := newHarness(t)
		code, out, _ := fixture.run("org", "list")
		if code != ExitWorked || !strings.Contains(out, "no organisations connected") {
			t.Fatalf("code %d out %q", code, out)
		}
		fixture.state.Current.Organizations = []machine.OrganizationRef{{Slug: "acme", Remote: "r1"}, {Slug: "other", Remote: "r2"}}
		fixture.state.Current.Active = "other"
		_, out, _ = fixture.run("org", "list")
		if !strings.Contains(out, "  acme  r1") || !strings.Contains(out, "* other  r2") {
			t.Fatalf("out %q", out)
		}
	})
	t.Run("use and disconnect print their outcomes", func(t *testing.T) {
		fixture := newHarness(t)
		clone := fixture.seedConnectedOrganization(t, 2)
		code, out, _ := fixture.run("org", "use", "acme")
		if code != ExitWorked || !strings.Contains(out, "active organisation: acme") {
			t.Fatalf("code %d out %q", code, out)
		}
		code, _, errOut := fixture.run("org", "use", "nobody")
		if code != ExitRefused || !strings.Contains(errOut, "not connected") {
			t.Fatalf("code %d err %q", code, errOut)
		}
		code, out, _ = fixture.run("org", "disconnect", "acme")
		if code != ExitWorked || !strings.Contains(out, "removed the clone at "+clone+" and 2 project registrations") || !strings.Contains(out, "no active organisation") {
			t.Fatalf("code %d out %q", code, out)
		}
	})
}

func TestStatus(t *testing.T) {
	t.Run("the report is printed, missing targets flagged", func(t *testing.T) {
		fixture := newHarness(t)
		fixture.state.Current.Placements = []machine.Placement{{Path: "/a", Target: filepath.Join(fixture.home, "nowhere"), Mode: "link", Owner: "import"}}
		fixture.state.Current.Organizations = []machine.OrganizationRef{{Slug: "acme", Remote: "r"}}
		fixture.state.Current.Active = "acme"
		code, out, _ := fixture.run("status")
		for _, expected := range []string{"github      Danidani23  (active gh account; slug danidani23)", "agents      fake-agent", "placements  1", "missing  import", "1 targets are missing", "* acme  r"} {
			if code != ExitWorked || !strings.Contains(out, expected) {
				t.Fatalf("missing %q: code %d out %q", expected, code, out)
			}
		}
	})
	t.Run("a logged-out identity is shown, not refused", func(t *testing.T) {
		fixture := newHarness(t)
		fixture.host = &fake.Host{}
		fixture.agent.Present = false
		code, out, _ := fixture.run("status")
		if code != ExitWorked || !strings.Contains(out, "github      none: gh is not logged in") || !strings.Contains(out, "agents      none found") {
			t.Fatalf("code %d out %q", code, out)
		}
	})
}

func TestReset(t *testing.T) {
	const placed = "/home/.claude/rules/global.md"
	t.Run("the summary lists the paths, ? explains, and a yes applies", func(t *testing.T) {
		fixture := newHarness(t)
		fixture.state.Current.Placements = []machine.Placement{{Path: placed}}
		code, out, _ := fixture.runInteractive("?\n\n", "reset")
		if code != ExitWorked || !strings.Contains(out, placed) || !strings.Contains(out, "A placement is one link") || !strings.Contains(out, "removed  "+placed) {
			t.Fatalf("code %d out %q", code, out)
		}
		if len(fixture.agent.Unplaced) != 1 || len(fixture.state.Current.Placements) != 0 {
			t.Fatalf("unplaced %v state %+v", fixture.agent.Unplaced, fixture.state.Current)
		}
	})
	t.Run("a no changes nothing", func(t *testing.T) {
		fixture := newHarness(t)
		fixture.state.Current.Placements = []machine.Placement{{Path: placed}}
		code, _, errOut := fixture.runInteractive("n\n", "reset")
		if code != ExitRefused || !strings.Contains(errOut, "nothing changed") || len(fixture.agent.Unplaced) != 0 {
			t.Fatalf("code %d err %q unplaced %v", code, errOut, fixture.agent.Unplaced)
		}
	})
	t.Run("without a terminal and without --yes it refuses", func(t *testing.T) {
		fixture := newHarness(t)
		fixture.state.Current.Placements = []machine.Placement{{Path: placed}}
		code, _, errOut := fixture.run("reset")
		if code != ExitRefused || !strings.Contains(errOut, "--yes") || len(fixture.agent.Unplaced) != 0 {
			t.Fatalf("code %d err %q", code, errOut)
		}
	})
	t.Run("a skipped placement is reported, kept, and exits 1", func(t *testing.T) {
		fixture := newHarness(t)
		fixture.state.Current.Placements = []machine.Placement{{Path: "/home/.claude/rules/foreign.md"}}
		code, _, errOut := fixture.run("reset", "--yes")
		if code != ExitRefused || !strings.Contains(errOut, "skipped  /home/.claude/rules/foreign.md: links elsewhere") || len(fixture.state.Current.Placements) != 1 {
			t.Fatalf("code %d err %q state %+v", code, errOut, fixture.state.Current)
		}
	})
	t.Run("--all names the organisations and projects and removes the clones", func(t *testing.T) {
		fixture := newHarness(t)
		clone := fixture.seedConnectedOrganization(t, 1)
		code, out, _ := fixture.run("reset", "--all", "--yes")
		if code != ExitWorked || !strings.Contains(out, "1 organisations with their clones under "+filepath.Join(fixture.home, "orgs")) || !strings.Contains(out, "removed  clone id-1") {
			t.Fatalf("code %d out %q", code, out)
		}
		_, err := os.Stat(clone)
		if err == nil || len(fixture.state.Current.Organizations) != 0 {
			t.Fatalf("clone %v state %+v", err, fixture.state.Current)
		}
	})
}

func TestOrgCreate(t *testing.T) {
	t.Run("with flags and --yes it creates, protects and connects, and prints each step", func(t *testing.T) {
		fixture := newHarness(t)
		code, out, errOut := fixture.run("org", "create", "--name", "Acme Corp", "--yes")
		if code != ExitWorked || !strings.Contains(out, "created Danidani23/acme-corp-agent-settings (private) at fake://Danidani23/acme-corp-agent-settings") || !strings.Contains(out, "protected main") || !strings.Contains(out, "connected acme-corp (Acme Corp) at "+filepath.Join(fixture.home, "orgs")) || !strings.Contains(out, "active organisation: acme-corp") {
			t.Fatalf("code %d out %q err %q", code, out, errOut)
		}
		if len(fixture.host.Created) != 1 || fixture.host.Created[0].Owner != "Danidani23" || fixture.host.Created[0].Description != "Agent settings of Acme Corp" {
			t.Fatalf("created %+v", fixture.host.Created)
		}
	})
	t.Run("the questions are asked in order and Enter takes every default", func(t *testing.T) {
		fixture := newHarness(t)
		code, out, errOut := fixture.runInteractive("Acme Corp\n\n\n\n\n\n", "org", "create")
		// the titles are styled, so the brackets are matched on their own
		for _, expected := range []string{"Organisation name", "[acme-corp-agent-settings]", "[Danidani23]", "1) private", "[Agent settings of Acme Corp]", "This will create Danidani23/acme-corp-agent-settings (private)", "[Y/n]", "created Danidani23/acme-corp-agent-settings"} {
			if code != ExitWorked || !strings.Contains(out, expected) {
				t.Fatalf("missing %q: code %d out %q err %q", expected, code, out, errOut)
			}
		}
	})
	t.Run("a no changes nothing", func(t *testing.T) {
		fixture := newHarness(t)
		code, _, errOut := fixture.runInteractive("Acme\n\n\n\n\nn\n", "org", "create")
		if code != ExitRefused || !strings.Contains(errOut, "nothing changed") || len(fixture.host.Created) != 0 {
			t.Fatalf("code %d err %q created %v", code, errOut, fixture.host.Created)
		}
	})
	t.Run("without a terminal it names the flags; a bad visibility is refused", func(t *testing.T) {
		fixture := newHarness(t)
		code, _, errOut := fixture.run("org", "create", "--name", "Acme")
		if code != ExitRefused || !strings.Contains(errOut, "--yes") {
			t.Fatalf("code %d err %q", code, errOut)
		}
		code, _, errOut = fixture.run("org", "create", "--name", "Acme", "--visibility", "secret", "--yes")
		if code != ExitRefused || !strings.Contains(errOut, "not private or public") || len(fixture.host.Created) != 0 {
			t.Fatalf("code %d err %q", code, errOut)
		}
	})
	t.Run("a refused protection is a warning, not a failure", func(t *testing.T) {
		fixture := newHarness(t)
		fixture.host.ProtectionRefusal = "Upgrade to GitHub Pro or make this repository public to enable this feature. (HTTP 403)"
		code, out, errOut := fixture.run("org", "create", "--name", "Acme", "--visibility", "public", "--owner", "acme-inc", "--repository", "settings", "--yes")
		if code != ExitWorked || !strings.Contains(errOut, "warning: main is not protected; GitHub said: Upgrade") || !strings.Contains(out, "created acme-inc/settings (public)") {
			t.Fatalf("code %d out %q err %q", code, out, errOut)
		}
	})
	t.Run("the identity gate holds", func(t *testing.T) {
		fixture := newHarness(t)
		fixture.host = &fake.Host{}
		code, _, errOut := fixture.run("org", "create", "--name", "Acme", "--yes")
		if code != ExitRefused || !strings.Contains(errOut, "gh auth login") {
			t.Fatalf("code %d err %q", code, errOut)
		}
	})
}

func TestTeamAndProjectCreate(t *testing.T) {
	t.Run("with flags and --yes both create records and say where", func(t *testing.T) {
		fixture := newHarness(t)
		_, _, _ = fixture.run("org", "create", "--name", "Acme", "--yes")
		code, out, errOut := fixture.run("team", "create", "--name", "Web", "--yes")
		if code != ExitWorked || !strings.Contains(out, "created team web (Web) in acme") || !strings.Contains(out, "record: declarations/organizations/acme/teams/web/team.yaml, pushed to main") {
			t.Fatalf("code %d out %q err %q", code, out, errOut)
		}
		code, out, errOut = fixture.run("project", "create", "--name", "Shop", "--team", "web", "--label", "language:go", "--yes")
		if code != ExitWorked || !strings.Contains(out, "created project shop (Shop) in acme for the team(s) web") || !strings.Contains(out, "record: declarations/organizations/acme/projects/shop/project.yaml, pushed to main") {
			t.Fatalf("code %d out %q err %q", code, out, errOut)
		}
	})
	t.Run("the project questions offer the teams and the registry, Enter takes the defaults", func(t *testing.T) {
		fixture := newHarness(t)
		_, _, _ = fixture.run("org", "create", "--name", "Acme", "--yes")
		code, out, errOut := fixture.runInteractive("Web\n\n\n", "team", "create")
		if code != ExitWorked || !strings.Contains(out, "[The Web team]") || !strings.Contains(out, "created team web (Web)") {
			t.Fatalf("code %d out %q err %q", code, out, errOut)
		}
		// name, description, teams (the one team is the default), then the categories in order: data none, kind none, language go; path, confirm
		code, out, errOut = fixture.runInteractive("Shop\n\n\n\n\n2\n\n\n", "project", "create")
		for _, expected := range []string{"1) web  ", "[web]", "Label data:", "Label kind:", "Label language:", "2) go", "[.]", "with labels language:go", "created project shop (Shop) in acme for the team(s) web"} {
			if code != ExitWorked || !strings.Contains(out, expected) {
				t.Fatalf("missing %q: code %d out %q err %q", expected, code, out, errOut)
			}
		}
	})
	t.Run("refusals: no organisation, no teams yet, an unknown team, no terminal", func(t *testing.T) {
		fixture := newHarness(t)
		code, _, errOut := fixture.run("team", "create", "--name", "Web", "--yes")
		if code != ExitRefused || !strings.Contains(errOut, "no active organisation") {
			t.Fatalf("code %d err %q", code, errOut)
		}
		_, _, _ = fixture.run("org", "create", "--name", "Acme", "--yes")
		code, _, errOut = fixture.run("project", "create", "--name", "Shop", "--team", "web", "--yes")
		if code != ExitRefused || !strings.Contains(errOut, "has no teams yet") {
			t.Fatalf("code %d err %q", code, errOut)
		}
		_, _, _ = fixture.run("team", "create", "--name", "Web", "--yes")
		code, _, errOut = fixture.run("project", "create", "--name", "Shop", "--team", "nope", "--yes")
		if code != ExitRefused || !strings.Contains(errOut, `team "nope" does not exist in acme; teams are web`) {
			t.Fatalf("code %d err %q", code, errOut)
		}
		code, _, errOut = fixture.run("project", "create", "--name", "Shop")
		if code != ExitRefused || !strings.Contains(errOut, "--team") {
			t.Fatalf("code %d err %q", code, errOut)
		}
	})
}
