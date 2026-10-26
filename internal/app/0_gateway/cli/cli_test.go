package cli

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
			// the two mains are where the standard streams enter, once each
			if strings.HasSuffix(filepath.ToSlash(filePath), "cmd/rein/main.go") || strings.HasSuffix(filepath.ToSlash(filePath), "cmd/reinctl/main.go") {
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
	releases   *fake.Releases
	lock       *fake.Lock
	keychain   *fake.Keychain
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	fixture := &harness{
		home:       t.TempDir(),
		host:       &fake.Host{Login: "Danidani23"},
		repository: &fake.Repository{Record: validRecord},
		state:      fake.NewState(),
		agent:      &fake.Agent{Present: true, HomePath: t.TempDir()},
		judge:      &fake.Judge{Unavailable: "no judge in this test"},
		releases:   &fake.Releases{Latest_: ports.Release{Version: "v0.1.0"}},
		lock:       &fake.Lock{},
		keychain:   &fake.Keychain{Tokens: map[string]ports.Token{"Danidani23": {Login: "Danidani23", AccessToken: "token-of-Danidani23", ExpiresAt: time.Now().Add(time.Hour)}}},
	}
	fixture.state.Current.Logins = []string{"Danidani23"}
	return fixture
}

func (fixture *harness) orchestrate() (orchestration.Orchestrator, error) {
	return orchestration.New(orchestration.NewServices(fixture.home, fixture.host, fixture.repository, fixture.state, []ports.Agent{fixture.agent}, fixture.judge, fixture.lock, fixture.keychain, fixture.releases, "v0.1.0", filepath.Join(fixture.home, "bin", "rein"))), nil
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
		fixture.state.Current.Logins = nil
		code, _, errOut := fixture.run("org", "list")
		if code != ExitRefused || !strings.Contains(errOut, "rein: not logged in") {
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
	t.Run("every org command passes the login gate first", func(t *testing.T) {
		fixture := newHarness(t)
		fixture.state.Current.Logins = nil
		for _, arguments := range [][]string{{"org", "connect", "x"}, {"org", "list"}, {"org", "use", "acme"}, {"org", "disconnect", "acme"}, {"org", "destroy", "acme", "--yes"}} {
			code, _, errOut := fixture.run(arguments...)
			if code != ExitRefused || !strings.Contains(errOut, "rein login") {
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
	t.Run("without a slug, use, disconnect and destroy offer the connected organisations", func(t *testing.T) {
		fixture := newHarness(t)
		code, _, errOut := fixture.run("org", "destroy", "--yes")
		if code != ExitRefused || !strings.Contains(errOut, "no organisations connected") {
			t.Fatalf("nothing connected: code %d err %q", code, errOut)
		}
		clone := fixture.seedConnectedOrganization(t, 0)
		fixture.state.Current.Organizations = append(fixture.state.Current.Organizations, machine.OrganizationRef{ID: "id-2", Slug: "other", Remote: "r2", Host: "github"})
		fixture.state.Current.OrgClonePaths["id-2"] = filepath.Join(fixture.home, "orgs", "id-2")
		code, _, errOut = fixture.run("org", "use")
		if code != ExitRefused || !strings.Contains(errOut, "name the organisation to make active; rein org list") {
			t.Fatalf("no terminal: code %d err %q", code, errOut)
		}
		code, out, errOut := fixture.runInteractive("2\n", "org", "use")
		if code != ExitWorked || !strings.Contains(out, "1) acme  \x1b[2;3mr1, active") || !strings.Contains(out, "2) other  \x1b[2;3mr2\x1b[0m\n") || !strings.Contains(out, "\x1b[1mChoose\x1b[0m: ") || !strings.Contains(out, "active organisation: other") {
			t.Fatalf("use: code %d out %q err %q", code, out, errOut)
		}
		code, out, errOut = fixture.runInteractive("\n", "org", "disconnect")
		if code != ExitRefused || !strings.Contains(out, "A choice is required") {
			t.Fatalf("several without a default: code %d out %q err %q", code, out, errOut)
		}
		code, out, errOut = fixture.runInteractive("other\n", "org", "disconnect")
		if code != ExitWorked || !strings.Contains(out, "disconnected other") || !strings.Contains(out, "active organisation: acme") {
			t.Fatalf("disconnect: code %d out %q err %q", code, out, errOut)
		}
		code, out, errOut = fixture.runInteractive("\n\n", "org", "destroy")
		if code != ExitWorked || !strings.Contains(out, "1) acme  \x1b[2;3mr1, active\x1b[0m  \x1b[2;3m(default)") || !strings.Contains(out, "disconnected acme: removed the clone at "+clone) {
			t.Fatalf("destroy with the only one as default: code %d out %q err %q", code, out, errOut)
		}
	})
	t.Run("destroy disconnects and prints the deletion steps, never deleting", func(t *testing.T) {
		fixture := newHarness(t)
		clone := fixture.seedConnectedOrganization(t, 1)
		fixture.state.Current.Organizations[0].Owner = "Danidani23"
		fixture.state.Current.Organizations[0].Repository = "acme-agent-settings"
		code, _, errOut := fixture.run("org", "destroy", "nobody", "--yes")
		if code != ExitRefused || !strings.Contains(errOut, "not connected") {
			t.Fatalf("unknown: code %d err %q", code, errOut)
		}
		code, _, errOut = fixture.run("org", "destroy", "acme")
		if code != ExitRefused || !strings.Contains(errOut, "pass --yes") || len(fixture.state.Current.Organizations) != 1 {
			t.Fatalf("no terminal: code %d err %q", code, errOut)
		}
		code, out, errOut := fixture.runInteractive("n\n", "org", "destroy", "acme")
		if code != ExitRefused || !strings.Contains(out, "This will disconnect acme on this machine: forget it, remove its clone at "+clone+" and forget 1 project registrations.") || !strings.Contains(out, "  /projects/0") || !strings.Contains(out, "The repository Danidani23/acme-agent-settings on GitHub stays; rein never deletes a repository.") || !strings.Contains(errOut, "nothing changed") || len(fixture.state.Current.Organizations) != 1 {
			t.Fatalf("declined: code %d out %q err %q", code, out, errOut)
		}
		code, out, errOut = fixture.runInteractive("\n", "org", "destroy", "acme")
		for _, expected := range []string{"disconnected acme: removed the clone at " + clone + " and 1 project registrations", "no active organisation", "To delete Danidani23/acme-agent-settings on GitHub by hand:", "1. Open https://github.com/Danidani23/acme-agent-settings/settings", "Danger Zone", "3. Type Danidani23/acme-agent-settings to confirm", "Afterwards rein reset machine"} {
			if code != ExitWorked || !strings.Contains(out, expected) {
				t.Fatalf("missing %q: code %d out %q err %q", expected, code, out, errOut)
			}
		}
		if len(fixture.state.Current.Organizations) != 0 || len(fixture.host.Repositories) != 0 {
			t.Fatalf("state %+v repositories %v", fixture.state.Current, fixture.host.Repositories)
		}
		_, err := os.Stat(clone)
		if err == nil {
			t.Fatalf("clone still there")
		}
	})
	t.Run("destroy of an organisation connected from a plain remote names the remote", func(t *testing.T) {
		fixture := newHarness(t)
		fixture.seedConnectedOrganization(t, 0)
		code, out, _ := fixture.run("org", "destroy", "acme", "--yes")
		if code != ExitWorked || !strings.Contains(out, "The remote r1 stays; rein never deletes a repository.") || !strings.Contains(out, "The remote r1 is untouched; delete it where it lives") {
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
		for _, expected := range []string{"logins      Danidani23", "agents      fake-agent", "placements  1", "missing  import", "1 targets are missing", "* acme  r"} {
			if code != ExitWorked || !strings.Contains(out, expected) {
				t.Fatalf("missing %q: code %d out %q", expected, code, out)
			}
		}
	})
	t.Run("a machine without a login is shown, not refused", func(t *testing.T) {
		fixture := newHarness(t)
		fixture.state.Current.Logins = nil
		fixture.agent.Present = false
		code, out, _ := fixture.run("status")
		if code != ExitWorked || !strings.Contains(out, "logins      none; run rein login") || !strings.Contains(out, "agents      none found") {
			t.Fatalf("code %d out %q", code, out)
		}
	})
}

func TestReset(t *testing.T) {
	const placed = "/home/.claude/rules/global.md"
	// a destructive command has no silent default, so the scope is named (D36)
	t.Run("alone it refuses, names both scopes and changes nothing", func(t *testing.T) {
		fixture := newHarness(t)
		fixture.state.Current.Placements = []machine.Placement{{Path: placed}}
		code, _, errOut := fixture.run("reset")
		if code != ExitRefused || !strings.Contains(errOut, "rein reset project") || !strings.Contains(errOut, "rein reset machine") || len(fixture.agent.Unplaced) != 0 {
			t.Fatalf("code %d err %q unplaced %v", code, errOut, fixture.agent.Unplaced)
		}
	})
	t.Run("the summary lists the paths, ? explains, and a yes applies", func(t *testing.T) {
		fixture := newHarness(t)
		fixture.state.Current.Placements = []machine.Placement{{Path: placed}}
		code, out, _ := fixture.runInteractive("?\n\n", "reset", "machine")
		if code != ExitWorked || !strings.Contains(out, placed) || !strings.Contains(out, "reset machine is reset project for everything") || !strings.Contains(out, "removed  "+placed) {
			t.Fatalf("code %d out %q", code, out)
		}
		if len(fixture.agent.Unplaced) != 1 || len(fixture.state.Current.Placements) != 0 {
			t.Fatalf("unplaced %v state %+v", fixture.agent.Unplaced, fixture.state.Current)
		}
	})
	t.Run("a no changes nothing", func(t *testing.T) {
		fixture := newHarness(t)
		fixture.state.Current.Placements = []machine.Placement{{Path: placed}}
		code, _, errOut := fixture.runInteractive("n\n", "reset", "machine")
		if code != ExitRefused || !strings.Contains(errOut, "nothing changed") || len(fixture.agent.Unplaced) != 0 {
			t.Fatalf("code %d err %q unplaced %v", code, errOut, fixture.agent.Unplaced)
		}
	})
	t.Run("without a terminal and without --yes it refuses", func(t *testing.T) {
		fixture := newHarness(t)
		fixture.state.Current.Placements = []machine.Placement{{Path: placed}}
		code, _, errOut := fixture.run("reset", "machine")
		if code != ExitRefused || !strings.Contains(errOut, "--yes") || len(fixture.agent.Unplaced) != 0 {
			t.Fatalf("code %d err %q", code, errOut)
		}
	})
	t.Run("a skipped placement is reported, kept, and exits 1", func(t *testing.T) {
		fixture := newHarness(t)
		fixture.state.Current.Placements = []machine.Placement{{Path: "/home/.claude/rules/foreign.md"}}
		code, _, errOut := fixture.run("reset", "machine", "--yes")
		if code != ExitRefused || !strings.Contains(errOut, "skipped  /home/.claude/rules/foreign.md: links elsewhere") || len(fixture.state.Current.Placements) != 1 {
			t.Fatalf("code %d err %q state %+v", code, errOut, fixture.state.Current)
		}
	})
	t.Run("the organisations, their clones and rein's folders go, and a project is put back first", func(t *testing.T) {
		fixture := newHarness(t)
		_, _, _ = fixture.run("org", "create", "--name", "Acme", "--yes")
		_, _, _ = fixture.run("team", "create", "--name", "Web", "--yes")
		_, _, _ = fixture.run("project", "create", "--name", "Shop", "--team", "web", "--label", "kind:service", "--yes")
		folder, _ := filepath.EvalSymlinks(t.TempDir())
		_ = fake.NewProjectClone(folder, []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, []string{"INSTRUCTIONS.md"})
		_ = os.WriteFile(filepath.Join(folder, "INSTRUCTIONS.md"), []byte("be brief"), 0o644)
		_, _, _ = fixture.run("init", "--path", folder, "--project", "shop", "--yes")
		code, out, errOut := fixture.run("reset", "machine", "--yes")
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

func TestResetProject(t *testing.T) {
	fixture := newHarness(t)
	_, _, _ = fixture.run("org", "create", "--name", "Acme", "--yes")
	_, _, _ = fixture.run("team", "create", "--name", "Web", "--yes")
	_, _, _ = fixture.run("project", "create", "--name", "Shop", "--team", "web", "--label", "kind:service", "--yes")
	folder, _ := filepath.EvalSymlinks(t.TempDir())
	_ = fake.NewProjectClone(folder, []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, []string{"INSTRUCTIONS.md"})
	_ = os.WriteFile(filepath.Join(folder, "INSTRUCTIONS.md"), []byte("be brief"), 0o644)
	_, _, _ = fixture.run("init", "--path", folder, "--project", "shop", "--yes")
	code, _, errOut := fixture.run("reset", "project", "--path", folder)
	if code != ExitRefused || !strings.Contains(errOut, "--yes") {
		t.Fatalf("code %d err %q", code, errOut)
	}
	code, out, errOut := fixture.run("reset", "project", "--path", folder, "--yes")
	for _, expected := range []string{"restored " + folder + ", formerly organizations.acme.projects.shop", "put back: INSTRUCTIONS.md", ".gitignore: rein block removed", "committed f4ce000: restore agent settings from rein (rein reset project)"} {
		if code != ExitWorked || !strings.Contains(out, expected) {
			t.Fatalf("missing %q: code %d out %q err %q", expected, code, out, errOut)
		}
	}
	code, _, errOut = fixture.run("reset", "project", "--path", folder, "--yes")
	if code != ExitRefused || !strings.Contains(errOut, "not initialised") {
		t.Fatalf("code %d err %q", code, errOut)
	}
}

func TestOrgCreate(t *testing.T) {
	t.Run("with flags and --yes it creates, protects and connects, and prints each step", func(t *testing.T) {
		fixture := newHarness(t)
		code, out, errOut := fixture.run("org", "create", "--name", "Acme Corp", "--yes")
		if code != ExitWorked || !strings.Contains(out, "created Danidani23/acme-corp-agent-settings (private) at https://github.com/Danidani23/acme-corp-agent-settings.git") || !strings.Contains(out, "protected main") || !strings.Contains(out, "connected acme-corp (Acme Corp) at "+filepath.Join(fixture.home, "orgs")) || !strings.Contains(out, "active organisation: acme-corp") {
			t.Fatalf("code %d out %q err %q", code, out, errOut)
		}
		if len(fixture.host.Created) != 1 || fixture.host.Created[0].Owner != "Danidani23" || fixture.host.Created[0].Description != "Agent settings of Acme Corp" {
			t.Fatalf("created %+v", fixture.host.Created)
		}
	})
	t.Run("the questions are asked in order and Enter takes every default", func(t *testing.T) {
		fixture := newHarness(t)
		// host (Enter: github), name, repository, owner (Enter: the account), visibility, description, confirm
		code, out, errOut := fixture.runInteractive("\nAcme Corp\n\n\n\n\n\n", "org", "create")
		// the titles are styled, so the brackets are matched on their own
		for _, expected := range []string{"1) github", "2) gitlab", "planned, not yet supported", "nothing of them goes to GitHub", "Organisation name", "[acme-corp-agent-settings]", "1) Danidani23  ", "your own account", "1) private", "[Agent settings of Acme Corp]", "This will create Danidani23/acme-corp-agent-settings (private) on GitHub as Danidani23", "[Y/n/e]", "created Danidani23/acme-corp-agent-settings"} {
			if code != ExitWorked || !strings.Contains(out, expected) {
				t.Fatalf("missing %q: code %d out %q err %q", expected, code, out, errOut)
			}
		}
	})
	t.Run("a no changes nothing", func(t *testing.T) {
		fixture := newHarness(t)
		code, _, errOut := fixture.runInteractive("\nAcme\n\n\n\n\nn\n", "org", "create")
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
	t.Run("the login gate holds without a terminal", func(t *testing.T) {
		fixture := newHarness(t)
		fixture.state.Current.Logins = nil
		code, _, errOut := fixture.run("org", "create", "--name", "Acme", "--yes")
		if code != ExitRefused || !strings.Contains(errOut, "rein login") {
			t.Fatalf("code %d err %q", code, errOut)
		}
	})
}

func TestTeamAndProjectCreate(t *testing.T) {
	t.Run("with flags and --yes both create records and say where", func(t *testing.T) {
		fixture := newHarness(t)
		_, _, _ = fixture.run("org", "create", "--name", "Acme", "--yes")
		code, out, errOut := fixture.run("team", "create", "--name", "Web", "--yes")
		if code != ExitWorked || !strings.Contains(out, "created team web (Web) in acme") || !strings.Contains(out, "record: declarations/teams/web/team.yaml, pushed to main") {
			t.Fatalf("code %d out %q err %q", code, out, errOut)
		}
		code, out, errOut = fixture.run("project", "create", "--name", "Shop", "--team", "web", "--label", "kind:service", "--label", "language:go", "--yes")
		if code != ExitWorked || !strings.Contains(out, "created project shop (Shop) in acme for the team(s) web") || !strings.Contains(out, "record: declarations/projects/shop/project.yaml, pushed to main") {
			t.Fatalf("code %d out %q err %q", code, out, errOut)
		}
	})
	t.Run("the project questions offer the teams and the registry, Enter takes the defaults", func(t *testing.T) {
		fixture := newHarness(t)
		_, _, _ = fixture.run("org", "create", "--name", "Acme", "--yes")
		code, out, errOut := fixture.runInteractive("Web\n\n\n", "team", "create")
		if code != ExitWorked || !strings.Contains(out, "One line about what the team does; Enter for none.") || strings.Contains(out, "[The Web team]") || !strings.Contains(out, "created team web (Web)") {
			t.Fatalf("code %d out %q err %q", code, out, errOut)
		}
		// name, description, teams (the one team is the default), the required kind, the other labels in one list, path, confirm
		code, out, errOut = fixture.runInteractive("Shop\n\n\n1\nlanguage:go\n\n\n", "project", "create")
		for _, expected := range []string{"1) web  ", "[web]", "Labels say what the project is, so declarations can apply to projects like it. Label kind, required: one value.", "1) cli  ", "Other labels, any that apply, several separated by commas; Enter for none.", "language:go  ", "data:public  ", "[.]", "with labels kind:cli, language:go", "created project shop (Shop) in acme for the team(s) web"} {
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
		code, _, errOut = fixture.run("project", "create", "--name", "Shop", "--team", "web", "--label", "kind:service", "--yes")
		if code != ExitRefused || !strings.Contains(errOut, "has no teams yet") {
			t.Fatalf("code %d err %q", code, errOut)
		}
		_, _, _ = fixture.run("team", "create", "--name", "Web", "--yes")
		code, _, errOut = fixture.run("project", "create", "--name", "Shop", "--team", "nope", "--label", "kind:cli", "--yes")
		if code != ExitRefused || !strings.Contains(errOut, `team "nope" does not exist in acme; teams are web`) {
			t.Fatalf("code %d err %q", code, errOut)
		}
		code, _, errOut = fixture.run("project", "create", "--name", "Shop", "--team", "web", "--label", "language:go", "--yes")
		if code != ExitRefused || !strings.Contains(errOut, "the label category kind is required; pass --label kind:<value>, one of cli, infrastructure, library, service, static-site, web-app") {
			t.Fatalf("required label: code %d err %q", code, errOut)
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
		_, _, _ = fixture.run("project", "create", "--name", "Shop", "--team", "web", "--label", "kind:service", "--yes")
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
		// project choice (create, the default), name, description, teams, the required kind, other labels, confirm
		code, out, errOut := fixture.runInteractive("\nShop\n\n\n1\n\n\n", "init", "--path", folder)
		for _, expected := range []string{"1) create a new project", "Project name\x1b[0m [" + filepath.Base(folder) + "]", "This will create the project \"Shop\" in acme and bind it", "created and bound " + folder + " to organizations.acme.projects.shop"} {
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
	_, _, _ = fixture.run("project", "create", "--name", "Shop", "--team", "web", "--label", "kind:service", "--yes")
	home := t.TempDir()
	_ = os.WriteFile(filepath.Join(home, "CLAUDE.md"), []byte("be brief"), 0o644)
	orchestrator, _ := fixture.orchestrate()
	_, _, err := orchestrator.Snapshots.Take("claude-code", "organizations.acme.projects.shop", home, []string{"CLAUDE.md"})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	code, out, errOut := fixture.run("distill", "--project", "shop", "--distiller", "none")
	for _, expected := range []string{"  ✓ reading the snapshot (", "  ✓ writing the proposal (", "distilled 1 candidates for organizations.acme.projects.shop", "new        rules/instructions", "1 new, 0 identical", "written to " + filepath.Join(home, ".rein", "proposals", "proposal-"), "next: review and edit it, then rein propose"} {
		if code != ExitWorked || !strings.Contains(out, expected) {
			t.Fatalf("missing %q: code %d out %q err %q", expected, code, out, errOut)
		}
	}
	code, _, errOut = fixture.run("distill", "--project", "shop", "--distiller", "none")
	if code != ExitRefused || !strings.Contains(errOut, "is pending at") || !strings.Contains(errOut, "--force") {
		t.Fatalf("pending twice: code %d err %q", code, errOut)
	}
	code, _, errOut = fixture.run("distill", "--project", "shop", "--force", "--distiller", "none")
	if code != ExitWorked {
		t.Fatalf("forced: code %d err %q", code, errOut)
	}
	code, out, _ = fixture.run("status")
	if code != ExitWorked || strings.Contains(out, "proposals pending") {
		// the snapshot's folder is no registered clone, so status does not look there
		t.Fatalf("status: code %d out %q", code, out)
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
	for _, expected := range []string{"proposed 1 declarations on branch rein/propose-acme-", "new        rules/brief  at organizations.acme", "pull request: https://github.com/Danidani23/acme-agent-settings/pull/1"} {
		if code != ExitWorked || !strings.Contains(out, expected) {
			t.Fatalf("missing %q: code %d out %q err %q", expected, code, out, errOut)
		}
	}
	if strings.Contains(out, "the proposal moved to") {
		t.Fatalf("a folder has no history file: %q", out)
	}
	code, out, errOut = fixture.runInteractive("\n", "propose", folder, "--level", "organizations.acme.teams.nowhere")
	if code != ExitWorked || !strings.Contains(out, "This will write 1 declarations from "+folder+" on a branch of the org clone of acme") || !strings.Contains(out, "  new        rules/brief  at organizations.acme.teams.nowhere\n") || !strings.Contains(out, "Continue?") {
		// the level's team need not exist for the proposal; the review decides
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
	code, _, errOut = fixture.run("propose", "--yes")
	if code != ExitRefused || !strings.Contains(errOut, "no pending proposal here") {
		t.Fatalf("no pending: code %d err %q", code, errOut)
	}
}

func TestProposePendingFile(t *testing.T) {
	fixture := newHarness(t)
	_, _, _ = fixture.run("org", "create", "--name", "Acme", "--yes")
	_, _, _ = fixture.run("team", "create", "--name", "Web", "--yes")
	_, _, _ = fixture.run("project", "create", "--name", "Shop", "--team", "web", "--label", "kind:service", "--yes")
	home := t.TempDir()
	_ = os.WriteFile(filepath.Join(home, "CLAUDE.md"), []byte("be brief"), 0o644)
	orchestrator, _ := fixture.orchestrate()
	_, _, _ = orchestrator.Snapshots.Take("claude-code", "organizations.acme.projects.shop", home, []string{"CLAUDE.md"})
	fixture.judge = &fake.Judge{Answers: []string{`{"version":2,"candidates":[{"candidate":"rules/instructions","verdict":"new","suggestion":"lift","level":"user","reason":"personal"}]}`}}
	code, out, errOut := fixture.run("distill", "--project", "shop")
	if code != ExitWorked || !strings.Contains(out, "distiller: new; lift to organizations.acme.users.danidani23: personal") {
		t.Fatalf("distill: code %d out %q err %q", code, out, errOut)
	}
	file := ""
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "written to ") {
			file = strings.TrimPrefix(line, "written to ")
		}
	}
	code, out, errOut = fixture.runInteractive("\n", "propose", file)
	if code != ExitWorked || !strings.Contains(out, "  new        rules/instructions  at organizations.acme.users.danidani23\n") || !strings.Contains(out, "proposed 1 declarations on branch rein/propose-shop-") || !strings.Contains(out, "the proposal moved to "+filepath.Join(fixture.home, "proposals", "acme")) {
		t.Fatalf("propose: code %d out %q err %q", code, out, errOut)
	}
	code, _, errOut = fixture.run("propose", file, "--yes")
	if code != ExitRefused || !strings.Contains(errOut, "cannot read") {
		t.Fatalf("gone: code %d err %q", code, errOut)
	}
}

func TestDistillJudgeFlags(t *testing.T) {
	fixture := newHarness(t)
	_, _, _ = fixture.run("org", "create", "--name", "Acme", "--yes")
	_, _, _ = fixture.run("team", "create", "--name", "Web", "--yes")
	_, _, _ = fixture.run("project", "create", "--name", "Shop", "--team", "web", "--label", "kind:service", "--yes")
	home := t.TempDir()
	_ = os.WriteFile(filepath.Join(home, "CLAUDE.md"), []byte("be brief"), 0o644)
	orchestrator, _ := fixture.orchestrate()
	_, _, _ = orchestrator.Snapshots.Take("claude-code", "organizations.acme.projects.shop", home, []string{"CLAUDE.md"})
	// a distiller that is not there refuses, naming the way without it (Q42)
	code, _, errOut := fixture.run("distill", "--project", "shop")
	if code != ExitRefused || !strings.Contains(errOut, "the distiller fake-judge is not available: no judge in this test") || !strings.Contains(errOut, "--distiller none") {
		t.Fatalf("code %d err %q", code, errOut)
	}
	code, out, _ := fixture.run("distill", "--project", "shop", "--distiller", "none")
	if code != ExitWorked || !strings.Contains(out, "distiller: fake-judge skipped: not asked") {
		t.Fatalf("code %d out %q", code, out)
	}
	fixture.judge = &fake.Judge{Answers: []string{`{"version":2,"candidates":[{"candidate":"rules/instructions","verdict":"new","suggestion":"lift","level":"organization","everywhere":"nothing in it is stack-specific","reason":"general"}]}`}}
	code, out, _ = fixture.run("distill", "--project", "shop", "--force")
	if code != ExitWorked || !strings.Contains(out, "distiller: new; lift to organizations.acme, every project: nothing in it is stack-specific: general") || !strings.Contains(out, "             reaches: shop\n") || !strings.Contains(out, "distiller: fake-judge answered\n") {
		t.Fatalf("code %d out %q", code, out)
	}
	// the last line is the command that opens the distiller's conversation (Q49)
	if !strings.HasSuffix(out, "\nthe distiller's conversation, to read or go on with it: fake-judge --resume fake-session\n") {
		t.Fatalf("no command for the conversation: out %q", out)
	}
	code, out, _ = fixture.run("distill", "--project", "shop", "--distiller", "none", "--force")
	if code != ExitWorked || !strings.Contains(out, "distiller: fake-judge skipped: not asked") || strings.Contains(out, "asking the distiller") {
		t.Fatalf("code %d out %q", code, out)
	}
	code, out, errOut = fixture.run("distill", "--project", "shop", "--dry-run")
	if code != ExitWorked || !strings.Contains(out, "# Candidates") || !strings.Contains(out, "## rules/instructions") || !strings.Contains(errOut, "dry run") {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
	code, _, errOut = fixture.run("distill", "--project", "shop", "--distiller", "oracle")
	if code != ExitRefused || !strings.Contains(errOut, `--distiller "oracle" is not agent-cli or none`) {
		t.Fatalf("code %d err %q", code, errOut)
	}
	// --judge, the flag's old name, still works
	code, out, _ = fixture.run("distill", "--project", "shop", "--judge", "none", "--force")
	if code != ExitWorked || !strings.Contains(out, "distiller: fake-judge skipped: not asked") {
		t.Fatalf("old flag: code %d out %q", code, out)
	}
	// a conflict is counted, and the next step is to resolve it
	_, orgClonePath, _ := orchestrator.Organizations.Lookup("acme")
	_ = os.MkdirAll(filepath.Join(orgClonePath, "declarations", "rules"), 0o755)
	_ = os.WriteFile(filepath.Join(orgClonePath, "declarations", "rules", "style.md"), []byte("Be verbose.\n"), 0o644)
	fixture.judge = &fake.Judge{Answers: []string{`{"version":2,"candidates":[{"candidate":"rules/instructions","verdict":"contradicts","related":["organizations.acme.rules.style"],"quotes":[{"address":"organizations.acme.rules.style","text":"Be verbose."}],"suggestion":"keep","reason":"the project wants it short"}]}`}}
	code, out, _ = fixture.run("distill", "--project", "shop", "--force")
	if code != ExitWorked || !strings.Contains(out, "1 in conflict, which rein propose waits for") || !strings.Contains(out, "next: set conflict.resolution in each file in conflict") {
		t.Fatalf("conflict: code %d out %q", code, out)
	}
	// the files in conflict close the output, each as its full path, after where the proposal went
	conflictFile := filepath.Join(home, ".rein", "proposals", "proposal-")
	written := strings.Index(out, "written to ")
	listed := strings.Index(out, "\n  "+conflictFile)
	if written < 0 || listed < written || !strings.Contains(out[listed:], filepath.FromSlash("/rule_instructions.yaml")+"  with organizations.acme.rules.style") {
		t.Fatalf("conflict files: out %q", out)
	}
}

func TestResolve(t *testing.T) {
	fixture := newHarness(t)
	_, _, _ = fixture.run("org", "create", "--name", "Acme", "--yes")
	_, _, _ = fixture.run("team", "create", "--name", "Web", "--yes")
	_, _, _ = fixture.run("project", "create", "--name", "Shop", "--team", "web", "--label", "kind:service", "--yes")
	code, out, _ := fixture.run("resolve", "--project", "shop")
	if code != ExitWorked || !strings.Contains(out, "organizations.acme.projects.shop: teams web, labels kind:service, user danidani23") || !strings.Contains(out, "no declarations apply yet") {
		t.Fatalf("code %d out %q", code, out)
	}
	orchestrator, _ := fixture.orchestrate()
	_, orgClonePath, _ := orchestrator.Organizations.Lookup("acme")
	write := func(relative string, content string) {
		target := filepath.Join(orgClonePath, "declarations", filepath.FromSlash(relative))
		_ = os.MkdirAll(filepath.Dir(target), 0o755)
		_ = os.WriteFile(target, []byte(content), 0o644)
	}
	write("rules/style.md", "---\nenforcement: required\n---\nStyle.\n")
	write("teams/web/rules/testing.md", "Web tests.\n")
	code, out, _ = fixture.run("resolve", "--project", "shop")
	for _, expected := range []string{"organizations.acme\n  rules        required  style", "organizations.acme.teams.web\n  rules        default   testing"} {
		if code != ExitWorked || !strings.Contains(out, expected) {
			t.Fatalf("missing %q: code %d out %q", expected, code, out)
		}
	}
	write("teams/web/rules/style.md", "Ours.\n")
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
	_, _, _ = fixture.run("project", "create", "--name", "Shop", "--team", "web", "--label", "kind:service", "--yes")
	orchestrator, _ := fixture.orchestrate()
	_, orgClonePath, _ := orchestrator.Organizations.Lookup("acme")
	rule := filepath.Join(orgClonePath, "declarations", "rules", "style.md")
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
	fixture.agent.Untrusted = true
	code, out, errOut = fixture.run("sync", "--path", folder)
	if code != ExitWorked || !strings.Contains(errOut, "warning: fake-agent has not trusted this folder yet, so rein's tool server stays pending; start it here once and accept the trust dialog:\n  cd "+folder+" && fake-agent") {
		t.Fatalf("untrusted folder: code %d out %q err %q", code, out, errOut)
	}
	code, out, errOut = fixture.run("sync", "--path", folder, "--hook")
	if code != ExitWorked || strings.Contains(errOut, "trust") {
		t.Fatalf("the hook run warns about trust: code %d out %q err %q", code, out, errOut)
	}
	fixture.agent.Untrusted = false
	code, out, _ = fixture.run("sync", "--path", folder, "--hook")
	if code != ExitWorked || !strings.Contains(out, "rein: organizations.acme.projects.shop synced for fake-agent, 0 files written, 5 unchanged") || strings.Contains(out, "just written") {
		t.Fatalf("hook: code %d out %q", code, out)
	}
	code, out, _ = fixture.run("status")
	if code != ExitWorked || !strings.Contains(out, "project clones  1") || !strings.Contains(out, "5 files from sync") {
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

func TestMachineLock(t *testing.T) {
	fixture := newHarness(t)
	_, _, _ = fixture.run("org", "create", "--name", "Acme", "--yes")
	if fixture.lock.Acquired != 1 || fixture.lock.Released != 1 {
		t.Fatalf("org create took the lock %d times and released it %d times", fixture.lock.Acquired, fixture.lock.Released)
	}
	fixture.lock.Holder = "4242"
	code, _, errOut := fixture.run("org", "use", "acme")
	if code != ExitRefused || !strings.Contains(errOut, "another rein is running (pid 4242") {
		t.Fatalf("code %d err %q", code, errOut)
	}
	code, out, _ := fixture.run("status")
	if code != ExitWorked || !strings.Contains(out, "* acme") {
		t.Fatalf("status under a held lock: code %d out %q", code, out)
	}
	code, out, _ = fixture.run("resolve", "--project", "nope")
	if code != ExitRefused || strings.Contains(out, "another rein") {
		t.Fatalf("resolve under a held lock: code %d out %q", code, out)
	}
}

func TestEditAtTheConfirmation(t *testing.T) {
	fixture := newHarness(t)
	// host, name, repository, owner, visibility, description, e; then name again with the default kept, the rest Enter, confirm
	code, out, errOut := fixture.runInteractive("\nAcme\n\n\n\n\ne\nAcme Two\n\n\n\n\n\n", "org", "create")
	if code != ExitWorked || !strings.Contains(out, "[Y/n/e]") || !strings.Contains(out, "Organisation name\x1b[0m [Acme]") || !strings.Contains(out, "for the organisation \"Acme Two\"") || !strings.Contains(out, "created Danidani23/acme-agent-settings") {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
	_, _, _ = fixture.run("team", "create", "--name", "Web", "--yes")
	// name, description, teams, kind, other labels, path, e; then name, description, teams, kind (previous kept), other labels, path, confirm
	code, out, errOut = fixture.runInteractive("Shop\n\n\n1\n\n\ne\n\n\n\n\nlanguage:go\n\n\n", "project", "create")
	if code != ExitWorked || !strings.Contains(out, "Project name\x1b[0m [Shop]") || !strings.Contains(out, "1) cli  \x1b[2;3ma command-line tool\x1b[0m  \x1b[2;3m(default)") || !strings.Contains(out, "with labels kind:cli, language:go") || !strings.Contains(out, "created project shop (Shop)") {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
}

func TestProgressLines(t *testing.T) {
	fixture := newHarness(t)
	code, out, _ := fixture.run("org", "create", "--name", "Acme", "--yes")
	if code != ExitWorked || !strings.Contains(out, "  ✓ creating the repository on the host (") || strings.Contains(out, "\x1b[") {
		t.Fatalf("file output: code %d out %q", code, out)
	}
	fixture = newHarness(t)
	code, out, _ = fixture.runInteractive("\nAcme\n\n\n\n\n\n", "org", "create")
	if code != ExitWorked || !strings.Contains(out, "\x1b[2mcreating the repository on the host …\x1b[0m") || !strings.Contains(out, "\r\x1b[2K  \x1b[32m✓ creating the repository on the host (") {
		t.Fatalf("terminal output: code %d out %q", code, out)
	}
	// an enclosing step keeps its start line and gets its outcome on a line of its own
	if !strings.Contains(out, "\x1b[2mconnecting this machine …\x1b[0m\n  \x1b[2mcloning the org repository …\x1b[0m") || !strings.Contains(out, "\n  \x1b[32m✓ connecting this machine (") {
		t.Fatalf("nested output: out %q", out)
	}
	fixture = newHarness(t)
	fixture.repository.FailPublish = true
	code, out, errOut := fixture.run("org", "create", "--name", "Acme", "--yes")
	if code == ExitWorked || !strings.Contains(out, "  ✗ pushing the first commit: git push failed") || !strings.Contains(out, "  ↩ undone writing the first content") {
		t.Fatalf("failure output: code %d out %q err %q", code, out, errOut)
	}
}

func TestProjectEditAndLabelRegistry(t *testing.T) {
	fixture := newHarness(t)
	_, _, _ = fixture.run("org", "create", "--name", "Acme", "--yes")
	_, _, _ = fixture.run("team", "create", "--name", "Web", "--yes")
	_, _, _ = fixture.run("project", "create", "--name", "Shop", "--team", "web", "--label", "kind:service", "--yes")
	code, _, errOut := fixture.run("project", "edit", "shop")
	if code != ExitRefused || !strings.Contains(errOut, "nothing to change; pass --description") {
		t.Fatalf("edit without flags off a terminal: code %d err %q", code, errOut)
	}
	code, _, errOut = fixture.run("project", "edit", "shop", "--label", "language:go", "--yes")
	if code != ExitRefused || !strings.Contains(errOut, "the label category kind is required") {
		t.Fatalf("edit dropping the required label: code %d err %q", code, errOut)
	}
	code, out, errOut := fixture.run("project", "edit", "shop", "--label", "kind:cli", "--label", "language:go", "--yes")
	if code != ExitWorked || !strings.Contains(out, "This will change the project shop in acme: description \"\", teams web, labels kind:cli, language:go, path .") || !strings.Contains(out, "changed project shop (Shop) in acme: labels") || !strings.Contains(out, "record: declarations/projects/shop/project.yaml, pushed to main") {
		t.Fatalf("edit with flags: code %d out %q err %q", code, out, errOut)
	}
	code, _, errOut = fixture.run("project", "edit", "shop", "--label", "kind:cli", "--label", "language:go", "--yes")
	if code != ExitRefused || !strings.Contains(errOut, "nothing changed on project shop") {
		t.Fatalf("edit without a change: code %d err %q", code, errOut)
	}
	// the one project is the default; description typed, teams, kind and other labels kept by Enter, path kept, confirm
	code, out, errOut = fixture.runInteractive("\nA web shop\n\n\n\n\n\n", "project", "edit")
	if code != ExitWorked || strings.Contains(out, "Project name") || !strings.Contains(out, "1) shop  \x1b[2;3mShop, teams web") || !strings.Contains(out, "\x1b[1mChoose\x1b[0m [1]: ") || !strings.Contains(out, "Choose any, or none\x1b[0m [language:go]") || !strings.Contains(out, "changed project shop (Shop) in acme: description") {
		t.Fatalf("interactive edit: code %d out %q err %q", code, out, errOut)
	}
	code, out, errOut = fixture.run("label", "list")
	if code != ExitWorked || !strings.Contains(out, "label registry of acme") || !strings.Contains(out, "kind  required: every project carries one\n  cli              a command-line tool\n") || !strings.Contains(out, "\nlanguage  several values allowed\n  dart") {
		t.Fatalf("list: code %d out %q err %q", code, out, errOut)
	}
	code, out, errOut = fixture.run("label", "add", "kind:worker", "--description", "a background worker", "--yes")
	if code != ExitWorked || !strings.Contains(out, "This will add kind:worker (a background worker) to the registry of acme") || !strings.Contains(out, "added kind:worker to the registry of acme") || !strings.Contains(out, "registry: declarations/labels/kind.yaml, pushed to main") {
		t.Fatalf("add: code %d out %q err %q", code, out, errOut)
	}
	code, _, errOut = fixture.run("label", "add", "kind:worker", "--yes")
	if code != ExitRefused || !strings.Contains(errOut, "kind:worker is already in the registry") {
		t.Fatalf("add twice: code %d err %q", code, errOut)
	}
	// category: 5 is the new one after data, kind, language, stack; then its name, the value, the description, required yes, several no, confirm
	code, out, errOut = fixture.runInteractive("5\ncloud\naws\nruns on AWS\ny\nn\n\n", "label", "add")
	if code != ExitWorked || !strings.Contains(out, "5) a new category") || !strings.Contains(out, "Must every project carry one value of cloud?") || !strings.Contains(out, "May a project carry several values of cloud?") || !strings.Contains(out, "This will create the category cloud in the registry of acme, required: every project carries one of its values, one value per project, with its first value aws (runs on AWS)") || !strings.Contains(out, "created the category cloud with cloud:aws in acme") {
		t.Fatalf("interactive add: code %d out %q err %q", code, out, errOut)
	}
	code, out, _ = fixture.run("label", "list")
	if !strings.Contains(out, "cloud  required") || !strings.Contains(out, "  aws              runs on AWS") || !strings.Contains(out, "  worker           a background worker") {
		t.Fatalf("list after: out %q", out)
	}
	code, _, errOut = fixture.run("label", "remove", "kind:cli", "--yes")
	if code != ExitRefused || !strings.Contains(errOut, "kind:cli is carried by the projects shop; change them first with rein project edit") {
		t.Fatalf("remove carried: code %d err %q", code, errOut)
	}
	code, _, errOut = fixture.run("label", "remove", "cloud:aws", "--yes")
	if code != ExitRefused || !strings.Contains(errOut, "the last value of its category") {
		t.Fatalf("remove last: code %d err %q", code, errOut)
	}
	code, out, errOut = fixture.run("label", "remove", "kind:worker", "--yes")
	if code != ExitWorked || !strings.Contains(out, "removed kind:worker from the registry of acme") {
		t.Fatalf("remove: code %d out %q err %q", code, out, errOut)
	}
	code, out, errOut = fixture.run("label", "set", "language", "--required", "--yes")
	if code != ExitWorked || !strings.Contains(out, "This will make the category language required, so every project created or edited from now on carries one of its values in acme") || !strings.Contains(out, "the category language in acme is required, several values per project") {
		t.Fatalf("set required: code %d out %q err %q", code, out, errOut)
	}
	code, _, errOut = fixture.run("project", "create", "--name", "Blog", "--team", "web", "--label", "kind:web-app", "--label", "cloud:aws", "--yes")
	if code != ExitRefused || !strings.Contains(errOut, "the label category language is required") {
		t.Fatalf("create after require: code %d err %q", code, errOut)
	}
	code, out, errOut = fixture.run("label", "set", "language", "--optional", "--single", "--yes")
	if code != ExitWorked || !strings.Contains(out, "This will make the category language optional and limited to one value per project in acme") || !strings.Contains(out, "the category language in acme is optional, one value per project") {
		t.Fatalf("set optional single: code %d out %q err %q", code, out, errOut)
	}
	code, _, errOut = fixture.run("label", "set", "language", "--yes")
	if code != ExitRefused || !strings.Contains(errOut, "say what to set") {
		t.Fatalf("set nothing: code %d err %q", code, errOut)
	}
	code, _, errOut = fixture.run("label", "set", "language", "--required", "--optional", "--yes")
	if code != ExitRefused || !strings.Contains(errOut, "exclude each other") {
		t.Fatalf("set both ways: code %d err %q", code, errOut)
	}
	code, _, errOut = fixture.run("project", "create", "--name", "Two", "--team", "web", "--label", "kind:cli", "--label", "data:internal", "--label", "data:public", "--yes")
	if code != ExitRefused || !strings.Contains(errOut, "data takes one value; given internal, public") {
		t.Fatalf("two values with flags: code %d err %q", code, errOut)
	}
	// name, description, teams, cloud and kind (single choices, both required by now), other labels twice: two data values refused, then one; path, confirm
	code, out, errOut = fixture.runInteractive("Three\n\n\n1\n1\ndata:internal,data:public\ndata:public\n\n\n", "project", "create")
	if code != ExitWorked || !strings.Contains(out, "Label kind, required: one value.") || !strings.Contains(out, "\x1b[1mChoose\x1b[0m: ") || !strings.Contains(out, "data takes one value; given internal, public. Other labels") || !strings.Contains(out, "with labels cloud:aws, kind:cli, data:public") {
		t.Fatalf("two values asked again: code %d out %q err %q", code, out, errOut)
	}
	code, _, errOut = fixture.run("label", "remove", "nope")
	if code != ExitRefused || !strings.Contains(errOut, "is not category:value") {
		t.Fatalf("bad label: code %d err %q", code, errOut)
	}
	code, _, errOut = fixture.run("label", "set", "nope", "--required", "--yes")
	if code != ExitRefused || !strings.Contains(errOut, `category "nope" is not in the registry; categories are cloud, data, kind, language, stack`) {
		t.Fatalf("unknown category: code %d err %q", code, errOut)
	}
}

func TestSpinner(t *testing.T) {
	var out bytes.Buffer
	environment := &commandEnvironment{out: &out, errOut: &out, terminal: true}
	environment.printProgress(orchestration.ProgressEvent{Kind: orchestration.ProgressStarted, Step: "asking the judge"})
	time.Sleep(3 * spinnerInterval)
	environment.printProgress(orchestration.ProgressEvent{Kind: orchestration.ProgressDone, Step: "asking the judge", Elapsed: 400 * time.Millisecond})
	text := out.String()
	if !strings.Contains(text, "\r\x1b[2K  \x1b[2m⠋\x1b[0m \x1b[2masking the judge …\x1b[0m \x1b[2m0s\x1b[0m") || !strings.HasSuffix(text, "\r\x1b[2K  \x1b[32m✓ asking the judge (400ms)\x1b[0m\n") || environment.spinner != nil {
		t.Fatalf("out %q", text)
	}
	quiet := &commandEnvironment{out: &out, errOut: &out}
	out.Reset()
	quiet.printProgress(orchestration.ProgressEvent{Kind: orchestration.ProgressStarted, Step: "step"})
	time.Sleep(2 * spinnerInterval)
	quiet.printProgress(orchestration.ProgressEvent{Kind: orchestration.ProgressDone, Step: "step"})
	if out.String() != "  ✓ step (0s)\n" {
		t.Fatalf("off a terminal: %q", out.String())
	}
}

func TestDeclarationCommands(t *testing.T) {
	fixture := newHarness(t)
	_, _, _ = fixture.run("org", "create", "--name", "Acme", "--yes")
	_, _, _ = fixture.run("team", "create", "--name", "Web", "--yes")
	code, out, _ := fixture.run("declaration", "list")
	if code != ExitWorked || !strings.Contains(out, "no declarations yet") {
		t.Fatalf("empty list: code %d out %q", code, out)
	}
	root := filepath.Join(fixture.state.Current.OrgClonePaths[fixture.state.Current.Organizations[0].ID], "declarations")
	for relative, content := range map[string]string{"users/danidani23/user.yaml": "slug: danidani23\ngithub: Danidani23\nname: Dani\n", "users/danidani23/skills/commit/SKILL.md": "---\nname: commit\ndescription: Draft commits.\n---\n\n# Commit\n", "rules/style.md": "---\napplyIf:\n  - language:go\n---\n\nHouse style.\n"} {
		target := filepath.Join(root, filepath.FromSlash(relative))
		_ = os.MkdirAll(filepath.Dir(target), 0o755)
		_ = os.WriteFile(target, []byte(content), 0o644)
	}
	_ = fixture.repository.Push(ports.RemoteAccess{}, filepath.Dir(root))
	code, out, errOut := fixture.run("declaration", "list")
	if code != ExitWorked || !strings.Contains(out, "ADDRESS                                            APPLY IF     DO NOT APPLY IF  ENFORCEMENT\n") || !strings.Contains(out, "organizations.acme.rules.style                     language:go                   default\n") || !strings.Contains(out, "organizations.acme.users.danidani23.skills.commit                                default\n") {
		t.Fatalf("list: code %d out %q err %q", code, out, errOut)
	}
	code, _, errOut = fixture.run("declaration", "edit", "organizations.acme.users.danidani23.skills.commit")
	if code != ExitRefused || !strings.Contains(errOut, "nothing to change; pass --level") {
		t.Fatalf("edit without flags: code %d err %q", code, errOut)
	}
	code, out, errOut = fixture.run("declaration", "edit", "organizations.acme.users.danidani23.skills.commit", "--level", "organizations.acme", "--apply-if", "language:go", "--yes")
	for _, expected := range []string{"This will open a pull request on the org repository of acme that puts organizations.acme.users.danidani23.skills.commit at organizations.acme with applyIf language:go, default.", "changed organizations.acme.users.danidani23.skills.commit: level, applyIf", "its address after the merge: organizations.acme.skills.commit", "pull request: https://github.com/Danidani23/acme-agent-settings/pull/1", "next: review and merge it"} {
		if code != ExitWorked || !strings.Contains(out, expected) {
			t.Fatalf("missing %q: code %d out %q err %q", expected, code, out, errOut)
		}
	}
	// level kept, applyIf cleared with Enter? no: Enter keeps the default; the answer "" on a multi-select means none only without a default, so type the new labels
	code, out, errOut = fixture.runInteractive("\nlanguage:go,language:rust\n\n2\n\n", "declaration", "edit", "organizations.acme.rules.style")
	if code != ExitWorked || !strings.Contains(out, "1) organizations.acme  \x1b[2;3mthe whole organisation\x1b[0m  \x1b[2;3m(default)") || !strings.Contains(out, "2) organizations.acme.teams.web  \x1b[2;3mthe team Web") || !strings.Contains(out, "3) organizations.acme.users.danidani23  \x1b[2;3myour own user level") || !strings.Contains(out, "Choose any, or none\x1b[0m [language:go]") || !strings.Contains(out, "1) default") || !strings.Contains(out, "puts organizations.acme.rules.style at organizations.acme with applyIf language:go language:rust, required") || !strings.Contains(out, "changed organizations.acme.rules.style: applyIf, enforcement") {
		t.Fatalf("interactive edit: code %d out %q err %q", code, out, errOut)
	}
	code, _, errOut = fixture.run("declaration", "edit", "organizations.acme.rules.style", "--apply-if", "", "--yes")
	if code != ExitWorked {
		t.Fatalf("clearing: code %d err %q", code, errOut)
	}
	code, _, errOut = fixture.run("declaration", "remove", "organizations.acme.rules.style")
	if code != ExitRefused || !strings.Contains(errOut, "pass --yes") {
		t.Fatalf("remove without a terminal: code %d err %q", code, errOut)
	}
	code, out, errOut = fixture.run("declaration", "remove", "organizations.acme.rules.style", "--yes")
	if code != ExitWorked || !strings.Contains(out, "This will open a pull request on the org repository of acme that removes organizations.acme.rules.style: rules/style.md.") || !strings.Contains(out, "removed organizations.acme.rules.style on branch rein/remove-style-") || !strings.Contains(out, "pull request: https://github.com/Danidani23/acme-agent-settings/pull/") {
		t.Fatalf("remove: code %d out %q err %q", code, out, errOut)
	}
	code, _, errOut = fixture.run("declaration", "remove", "organizations.acme.rules.nope", "--yes")
	if code != ExitRefused || !strings.Contains(errOut, "no declaration at organizations.acme.rules.nope") {
		t.Fatalf("missing: code %d err %q", code, errOut)
	}
}

func TestInterruptReleasesTheLock(t *testing.T) {
	fixture := newHarness(t)
	environment := &commandEnvironment{in: strings.NewReader(""), out: &bytes.Buffer{}, errOut: &bytes.Buffer{}, orchestrate: fixture.orchestrate}
	_, release, err := environment.orchestrateLocked()
	if err != nil || environment.releaseLock == nil || fixture.lock.Acquired != 1 || fixture.lock.Released != 0 {
		t.Fatalf("lock: err %v acquired %d released %d", err, fixture.lock.Acquired, fixture.lock.Released)
	}
	environment.releaseLockNow()
	if fixture.lock.Released != 1 || environment.releaseLock != nil {
		t.Fatalf("the interrupt did not release the lock: released %d", fixture.lock.Released)
	}
	release()
	if fixture.lock.Released != 1 {
		t.Fatalf("released twice: %d", fixture.lock.Released)
	}
}

func TestTeamAndProjectListAndTeamEdit(t *testing.T) {
	fixture := newHarness(t)
	_, _, _ = fixture.run("org", "create", "--name", "Acme", "--yes")
	code, out, _ := fixture.run("team", "list")
	if code != ExitWorked || !strings.Contains(out, "acme has no teams yet") {
		t.Fatalf("no teams: code %d out %q", code, out)
	}
	_, _, _ = fixture.run("team", "create", "--name", "Web", "--description", "the web", "--yes")
	_, _, _ = fixture.run("project", "create", "--name", "Shop", "--team", "web", "--label", "kind:service", "--label", "language:go", "--yes")
	code, out, errOut := fixture.run("team", "list")
	if code != ExitWorked || !strings.Contains(out, "SLUG  NAME  DESCRIPTION  PROJECTS\n") || !strings.Contains(out, "web   Web   the web      shop\n") {
		t.Fatalf("team list: code %d out %q err %q", code, out, errOut)
	}
	code, out, errOut = fixture.run("project", "list")
	if code != ExitWorked || !strings.Contains(out, "SLUG  NAME  TEAMS  LABELS                    PATH  DESCRIPTION\n") || !strings.Contains(out, "shop  Shop  web    kind:service language:go  .\n") {
		t.Fatalf("project list: code %d out %q err %q", code, out, errOut)
	}
	code, _, errOut = fixture.run("team", "edit", "web")
	if code != ExitRefused || !strings.Contains(errOut, "nothing to change; pass --description") {
		t.Fatalf("edit off a terminal: code %d err %q", code, errOut)
	}
	code, out, errOut = fixture.run("team", "edit", "web", "--description", "the web shop", "--yes")
	if code != ExitWorked || !strings.Contains(out, "This will change the description of team web in acme to \"the web shop\"") || !strings.Contains(out, "changed team web (Web) in acme: description") || !strings.Contains(out, "record: declarations/teams/web/team.yaml, pushed to main") {
		t.Fatalf("edit with the flag: code %d out %q err %q", code, out, errOut)
	}
	// the one team is the default; the description typed; confirm
	code, out, errOut = fixture.runInteractive("\nthe shop on the web\n\n", "team", "edit")
	if code != ExitWorked || !strings.Contains(out, "1) web  \x1b[2;3mWeb\x1b[0m  \x1b[2;3m(default)") || !strings.Contains(out, "Description\x1b[0m [the web shop]") || !strings.Contains(out, "changed team web (Web) in acme: description") {
		t.Fatalf("interactive edit: code %d out %q err %q", code, out, errOut)
	}
	code, _, errOut = fixture.run("team", "edit", "web", "--description", "the shop on the web", "--yes")
	if code != ExitRefused || !strings.Contains(errOut, "nothing changed on team web") {
		t.Fatalf("no change: code %d err %q", code, errOut)
	}
}
