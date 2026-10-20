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
	judge      *fake.Judge
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	return &harness{
		home:       t.TempDir(),
		host:       &fake.Host{Login: "Danidani23"},
		repository: &fake.Repository{Record: validRecord},
		state:      fake.NewState(),
		agent:      &fake.Agent{Present: true, HomePath: t.TempDir()},
		judge:      &fake.Judge{Unavailable: "no judge in this test"},
	}
}

func (fixture *harness) orchestrate() (orchestration.Orchestrator, error) {
	return orchestration.New(orchestration.NewServices(fixture.home, fixture.host, fixture.repository, fixture.state, []ports.Agent{fixture.agent}, fixture.judge)), nil
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
		fixture.state.Current.ProjectClones = append(fixture.state.Current.ProjectClones, machine.ProjectClone{Organization: "id-1", ClonePath: fmt.Sprintf("/projects/%d", index)})
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
		if code != ExitWorked || !strings.Contains(out, placed) || !strings.Contains(out, "reset is restore for everything") || !strings.Contains(out, "removed  "+placed) {
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
	t.Run("the organisations, their clones and rein's folders go, and a project is put back first", func(t *testing.T) {
		fixture := newHarness(t)
		_, _, _ = fixture.run("org", "create", "--name", "Acme", "--yes")
		_, _, _ = fixture.run("team", "create", "--name", "Web", "--yes")
		_, _, _ = fixture.run("project", "create", "--name", "Shop", "--team", "web", "--yes")
		folder, _ := filepath.EvalSymlinks(t.TempDir())
		_ = fake.NewProjectClone(folder, []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, []string{"INSTRUCTIONS.md"})
		_ = os.WriteFile(filepath.Join(folder, "INSTRUCTIONS.md"), []byte("be brief"), 0o644)
		_, _, _ = fixture.run("init", "--path", folder, "--project", "shop", "--yes")
		code, out, errOut := fixture.run("reset", "--yes")
		for _, expected := range []string{"put back 1 initialised projects", "restored " + folder + ", formerly organizations.acme.projects.shop", "put back: INSTRUCTIONS.md", "committed f4ce000", "forget 1 organisations", "removed  clone ", "removed  " + filepath.Join(fixture.home, "orgs"), "reset done"} {
			if code != ExitWorked || !strings.Contains(out, expected) {
				t.Fatalf("missing %q: code %d out %q err %q", expected, code, out, errOut)
			}
		}
		content, _ := os.ReadFile(filepath.Join(folder, "INSTRUCTIONS.md"))
		_, err := os.Stat(filepath.Join(fixture.home, "orgs"))
		if string(content) != "be brief" || err == nil || len(fixture.state.Current.Organizations) != 0 {
			t.Fatalf("content %q orgs err %v state %+v", content, err, fixture.state.Current)
		}
	})
}

func TestRestore(t *testing.T) {
	fixture := newHarness(t)
	_, _, _ = fixture.run("org", "create", "--name", "Acme", "--yes")
	_, _, _ = fixture.run("team", "create", "--name", "Web", "--yes")
	_, _, _ = fixture.run("project", "create", "--name", "Shop", "--team", "web", "--yes")
	folder, _ := filepath.EvalSymlinks(t.TempDir())
	_ = fake.NewProjectClone(folder, []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, []string{"INSTRUCTIONS.md"})
	_ = os.WriteFile(filepath.Join(folder, "INSTRUCTIONS.md"), []byte("be brief"), 0o644)
	_, _, _ = fixture.run("init", "--path", folder, "--project", "shop", "--yes")
	code, _, errOut := fixture.run("restore", "--path", folder)
	if code != ExitRefused || !strings.Contains(errOut, "--yes") {
		t.Fatalf("code %d err %q", code, errOut)
	}
	code, out, errOut := fixture.run("restore", "--path", folder, "--yes")
	for _, expected := range []string{"restored " + folder + ", formerly organizations.acme.projects.shop", "put back: INSTRUCTIONS.md", ".gitignore: rein block removed", "committed f4ce000: restore agent settings from rein"} {
		if code != ExitWorked || !strings.Contains(out, expected) {
			t.Fatalf("missing %q: code %d out %q err %q", expected, code, out, errOut)
		}
	}
	code, _, errOut = fixture.run("restore", "--path", folder, "--yes")
	if code != ExitRefused || !strings.Contains(errOut, "not initialised") {
		t.Fatalf("code %d err %q", code, errOut)
	}
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

func TestInit(t *testing.T) {
	const rootCommit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	projectClone := func(t *testing.T) string {
		t.Helper()
		folder, _ := filepath.EvalSymlinks(t.TempDir())
		_ = fake.NewProjectClone(folder, []string{rootCommit}, []string{"INSTRUCTIONS.md"})
		_ = os.WriteFile(filepath.Join(folder, "INSTRUCTIONS.md"), []byte("be brief"), 0o644)
		return folder
	}
	t.Run("with --project and --yes it binds, moves out and commits, and says so", func(t *testing.T) {
		fixture := newHarness(t)
		_, _, _ = fixture.run("org", "create", "--name", "Acme", "--yes")
		_, _, _ = fixture.run("team", "create", "--name", "Web", "--yes")
		_, _, _ = fixture.run("project", "create", "--name", "Shop", "--team", "web", "--yes")
		folder := projectClone(t)
		code, out, errOut := fixture.run("init", "--path", folder, "--project", "shop", "--yes")
		for _, expected := range []string{"bound " + folder + " to organizations.acme.projects.shop (fingerprint: 1 root commit(s))", "moved out for fake-agent: INSTRUCTIONS.md", "snapshot: " + filepath.Join(fixture.home, "snapshots"), ".gitignore: rein block added", "committed f4ce000: move agent settings out to rein", "next: rein sync"} {
			if code != ExitWorked || !strings.Contains(out, expected) {
				t.Fatalf("missing %q: code %d out %q err %q", expected, code, out, errOut)
			}
		}
		code, _, errOut = fixture.run("init", "--path", folder, "--yes")
		if code != ExitRefused || !strings.Contains(errOut, "already initialised as organizations.acme.projects.shop") {
			t.Fatalf("code %d err %q", code, errOut)
		}
	})
	t.Run("without a terminal and without a match it names --project; outside a repository it refuses", func(t *testing.T) {
		fixture := newHarness(t)
		_, _, _ = fixture.run("org", "create", "--name", "Acme", "--yes")
		code, _, errOut := fixture.run("init", "--path", projectClone(t), "--yes")
		if code != ExitRefused || !strings.Contains(errOut, "--project") {
			t.Fatalf("code %d err %q", code, errOut)
		}
		code, _, errOut = fixture.run("init", "--path", t.TempDir(), "--yes")
		if code != ExitRefused || !strings.Contains(errOut, "not inside a git repository") {
			t.Fatalf("code %d err %q", code, errOut)
		}
	})
	t.Run("interactively it offers to create the project and asks its questions", func(t *testing.T) {
		fixture := newHarness(t)
		_, _, _ = fixture.run("org", "create", "--name", "Acme", "--yes")
		_, _, _ = fixture.run("team", "create", "--name", "Web", "--yes")
		folder := projectClone(t)
		// project choice (create, the default), name, description, teams, data, kind, language, confirm
		code, out, errOut := fixture.runInteractive("\nShop\n\n\n\n\n2\n\n", "init", "--path", folder)
		for _, expected := range []string{"1) create a new project", "Project name", "This will create the project \"Shop\" in acme and bind it", "created and bound " + folder + " to organizations.acme.projects.shop"} {
			if code != ExitWorked || !strings.Contains(out, expected) {
				t.Fatalf("missing %q: code %d out %q err %q", expected, code, out, errOut)
			}
		}
	})
}

func TestDistill(t *testing.T) {
	fixture := newHarness(t)
	_, _, _ = fixture.run("org", "create", "--name", "Acme", "--yes")
	_, _, _ = fixture.run("team", "create", "--name", "Web", "--yes")
	_, _, _ = fixture.run("project", "create", "--name", "Shop", "--team", "web", "--yes")
	home := t.TempDir()
	_ = os.WriteFile(filepath.Join(home, "CLAUDE.md"), []byte("be brief"), 0o644)
	orchestrator, _ := fixture.orchestrate()
	_, _, err := orchestrator.Snapshots.Take("claude-code", "organizations.acme.projects.shop", home, []string{"CLAUDE.md"})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	code, out, errOut := fixture.run("distill", "--project", "shop")
	for _, expected := range []string{"distilled 1 candidates for organizations.acme.projects.shop", "new        rules/instructions", "1 new, 0 identical", "written to " + filepath.Join(fixture.home, "distill", "shop-"), "next: rein propose"} {
		if code != ExitWorked || !strings.Contains(out, expected) {
			t.Fatalf("missing %q: code %d out %q err %q", expected, code, out, errOut)
		}
	}
	code, _, errOut = fixture.run("distill", "--path", t.TempDir())
	if code != ExitRefused || !strings.Contains(errOut, "--project") {
		t.Fatalf("code %d err %q", code, errOut)
	}
}

func TestPropose(t *testing.T) {
	fixture := newHarness(t)
	_, _, _ = fixture.run("org", "create", "--name", "Acme", "--yes")
	folder := t.TempDir()
	_ = os.MkdirAll(filepath.Join(folder, "rules"), 0o755)
	_ = os.WriteFile(filepath.Join(folder, "rules", "brief.md"), []byte("Be brief.\n"), 0o644)
	code, _, errOut := fixture.run("propose", folder)
	if code != ExitRefused || !strings.Contains(errOut, "--yes") {
		t.Fatalf("code %d err %q", code, errOut)
	}
	code, out, errOut := fixture.run("propose", folder, "--level", "organizations.acme", "--yes")
	for _, expected := range []string{"proposed 1 declarations to organizations.acme on branch rein/propose-acme-", "new        rules/brief", "pull request: https://github.com/fake/pull/1"} {
		if code != ExitWorked || !strings.Contains(out, expected) {
			t.Fatalf("missing %q: code %d out %q err %q", expected, code, out, errOut)
		}
	}
	code, out, errOut = fixture.runInteractive("\n", "propose", folder, "--level", "organizations.acme.teams.nowhere")
	if code != ExitWorked || !strings.Contains(out, "Continue?") {
		// the level's team need not exist for the proposal; the review decides
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
}

func TestDistillJudgeFlags(t *testing.T) {
	fixture := newHarness(t)
	_, _, _ = fixture.run("org", "create", "--name", "Acme", "--yes")
	_, _, _ = fixture.run("team", "create", "--name", "Web", "--yes")
	_, _, _ = fixture.run("project", "create", "--name", "Shop", "--team", "web", "--yes")
	home := t.TempDir()
	_ = os.WriteFile(filepath.Join(home, "CLAUDE.md"), []byte("be brief"), 0o644)
	orchestrator, _ := fixture.orchestrate()
	_, _, _ = orchestrator.Snapshots.Take("claude-code", "organizations.acme.projects.shop", home, []string{"CLAUDE.md"})
	code, out, _ := fixture.run("distill", "--project", "shop")
	if code != ExitWorked || !strings.Contains(out, "judge: fake-judge skipped: no judge in this test") {
		t.Fatalf("code %d out %q", code, out)
	}
	fixture.judge = &fake.Judge{Answers: []string{`{"version":1,"candidates":[{"candidate":"rules/instructions","verdict":"new","suggestion":"lift","level":"organization","reason":"general"}]}`}}
	code, out, _ = fixture.run("distill", "--project", "shop")
	if code != ExitWorked || !strings.Contains(out, "judge: new; lift to the organization level: general") || !strings.Contains(out, "judge: fake-judge answered") {
		t.Fatalf("code %d out %q", code, out)
	}
	code, out, _ = fixture.run("distill", "--project", "shop", "--judge", "none")
	if code != ExitWorked || strings.Contains(out, "judge:") {
		t.Fatalf("code %d out %q", code, out)
	}
	code, out, errOut := fixture.run("distill", "--project", "shop", "--dry-run")
	if code != ExitWorked || !strings.Contains(out, "# Candidates") || !strings.Contains(out, "## rules/instructions") || !strings.Contains(errOut, "dry run") {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
	code, _, errOut = fixture.run("distill", "--project", "shop", "--judge", "oracle")
	if code != ExitRefused || !strings.Contains(errOut, "not agent-cli or none") {
		t.Fatalf("code %d err %q", code, errOut)
	}
}

func TestResolve(t *testing.T) {
	fixture := newHarness(t)
	_, _, _ = fixture.run("org", "create", "--name", "Acme", "--yes")
	_, _, _ = fixture.run("team", "create", "--name", "Web", "--yes")
	_, _, _ = fixture.run("project", "create", "--name", "Shop", "--team", "web", "--yes")
	code, out, _ := fixture.run("resolve", "--project", "shop")
	if code != ExitWorked || !strings.Contains(out, "organizations.acme.projects.shop: teams web, labels none, user danidani23") || !strings.Contains(out, "no declarations apply yet") {
		t.Fatalf("code %d out %q", code, out)
	}
	orchestrator, _ := fixture.orchestrate()
	_, orgClonePath, _ := orchestrator.Organizations.Lookup("acme")
	write := func(relative string, content string) {
		target := filepath.Join(orgClonePath, "declarations", filepath.FromSlash(relative))
		_ = os.MkdirAll(filepath.Dir(target), 0o755)
		_ = os.WriteFile(target, []byte(content), 0o644)
	}
	write("organizations/acme/rules/style.md", "---\nenforcement: required\n---\nStyle.\n")
	write("organizations/acme/teams/web/rules/testing.md", "Web tests.\n")
	code, out, _ = fixture.run("resolve", "--project", "shop")
	for _, expected := range []string{"organizations.acme\n  rules        required  style", "organizations.acme.teams.web\n  rules        default   testing"} {
		if code != ExitWorked || !strings.Contains(out, expected) {
			t.Fatalf("missing %q: code %d out %q", expected, code, out)
		}
	}
	write("organizations/acme/teams/web/rules/style.md", "Ours.\n")
	code, _, errOut := fixture.run("resolve", "--project", "shop")
	if code != ExitRefused || !strings.Contains(errOut, "problem  organizations.acme.teams.web.rules.style: redeclares") || !strings.Contains(errOut, "sync would refuse") {
		t.Fatalf("code %d err %q", code, errOut)
	}
	code, _, errOut = fixture.run("resolve")
	if code != ExitRefused || !strings.Contains(errOut, "--project") {
		t.Fatalf("code %d err %q", code, errOut)
	}
}

func TestSync(t *testing.T) {
	fixture := newHarness(t)
	fixture.agent.HarnessFormat = "claude-code"
	_, _, _ = fixture.run("org", "create", "--name", "Acme", "--yes")
	_, _, _ = fixture.run("team", "create", "--name", "Web", "--yes")
	_, _, _ = fixture.run("project", "create", "--name", "Shop", "--team", "web", "--yes")
	orchestrator, _ := fixture.orchestrate()
	_, orgClonePath, _ := orchestrator.Organizations.Lookup("acme")
	rule := filepath.Join(orgClonePath, "declarations", "organizations", "acme", "rules", "style.md")
	_ = os.MkdirAll(filepath.Dir(rule), 0o755)
	_ = os.WriteFile(rule, []byte("House style.\n"), 0o644)
	folder, _ := filepath.EvalSymlinks(t.TempDir())
	_ = fake.NewProjectClone(folder, []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, []string{"CLAUDE.md"})
	_ = os.WriteFile(filepath.Join(folder, "CLAUDE.md"), []byte("old"), 0o644)
	_, _, _ = fixture.run("init", "--path", folder, "--project", "shop", "--yes")
	code, out, errOut := fixture.run("sync", "--path", folder)
	for _, expected := range []string{"synced organizations.acme.projects.shop for fake-agent", "written    CLAUDE.md", "written    .claude/rein/organization.md", "written    .claude/settings.local.json"} {
		if code != ExitWorked || !strings.Contains(out, expected) {
			t.Fatalf("missing %q: code %d out %q err %q", expected, code, out, errOut)
		}
	}
	code, out, _ = fixture.run("sync", "--path", folder, "--hook")
	if code != ExitWorked || !strings.Contains(out, "rein: organizations.acme.projects.shop synced for fake-agent, 0 files written, 3 unchanged") || strings.Contains(out, "just written") {
		t.Fatalf("hook: code %d out %q", code, out)
	}
	code, out, _ = fixture.run("status")
	if code != ExitWorked || !strings.Contains(out, "project clones  1") || !strings.Contains(out, "3 files from sync") {
		t.Fatalf("status: code %d out %q", code, out)
	}
	_ = os.WriteFile(filepath.Join(folder, "CLAUDE.md"), []byte("mine"), 0o644)
	code, _, errOut = fixture.run("sync", "--path", folder)
	if code != ExitRefused || !strings.Contains(errOut, "edited by hand") {
		t.Fatalf("code %d err %q", code, errOut)
	}
	code, out, _ = fixture.run("sync", "--path", folder, "--hook")
	if code != ExitWorked || !strings.Contains(out, "rein: ") || !strings.Contains(out, "edited by hand") {
		t.Fatalf("hook refusal: code %d out %q", code, out)
	}
	code, out, _ = fixture.run("sync", "--path", folder, "--force")
	if code != ExitWorked || !strings.Contains(out, "your edits to CLAUDE.md were snapshotted to") {
		t.Fatalf("force: code %d out %q", code, out)
	}
}
