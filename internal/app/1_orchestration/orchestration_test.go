package orchestration

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// The fakes: each port with the least behaviour a workflow needs. The
// services are real; only the edges are faked.

type fakeHost struct {
	login string
}

func (host fakeHost) Identity() (ports.Identity, error) {
	if host.login == "" {
		return ports.Identity{}, errors.New("gh is not logged in to GitHub; run gh auth login, then try again")
	}
	return ports.Identity{Login: host.login, Slug: strings.ToLower(host.login)}, nil
}

// fakeRepository "clones" by writing the organisation record it was given.
type fakeRepository struct {
	record string
	fail   bool
}

func (fakeRepository) Preflight() error { return nil }

func (repository fakeRepository) Clone(remote string, directory string) error {
	if repository.fail {
		return errors.New("git clone failed: repository not found")
	}
	folder := filepath.Join(directory, "declarations", "organizations", "acme")
	err := os.MkdirAll(folder, 0o755)
	if err != nil {
		return err
	}
	if repository.record == "" {
		return nil
	}
	return os.WriteFile(filepath.Join(folder, "organization.yaml"), []byte(repository.record), 0o644)
}

type fakeAgent struct {
	installed bool
	unplaced  []string
}

func (agent *fakeAgent) Name() string    { return "fake-agent" }
func (agent *fakeAgent) Installed() bool { return agent.installed }
func (agent *fakeAgent) Unplace(placed machine.Placement) (bool, string, error) {
	agent.unplaced = append(agent.unplaced, placed.Path)
	if strings.Contains(placed.Path, "foreign") {
		return false, "links elsewhere", nil
	}
	return true, "", nil
}

// memoryState keeps the state in memory and can fail on demand, which is how
// a compensation gets exercised.
type memoryState struct {
	state     machine.State
	failSaves int
}

func (memory *memoryState) Load() (machine.State, error) { return memory.state, nil }
func (memory *memoryState) Save(state machine.State) error {
	if memory.failSaves > 0 {
		memory.failSaves--
		return errors.New("state: disk full")
	}
	memory.state = state
	return nil
}

const validRecord = "id: 0f8fad5b-d9cb-469f-a165-70867728950e\nslug: acme\nname: Acme\nhost: github\n"

type fixture struct {
	home       string
	host       fakeHost
	repository fakeRepository
	state      *memoryState
	agent      *fakeAgent
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	return &fixture{
		home:       t.TempDir(),
		host:       fakeHost{login: "Danidani23"},
		repository: fakeRepository{record: validRecord},
		state:      &memoryState{state: machine.Empty()},
		agent:      &fakeAgent{installed: true},
	}
}

func (f *fixture) orchestrator() Orchestrator {
	return NewLocal(NewServices(f.home, f.host, f.repository, f.state, []ports.Agent{f.agent}))
}

func (f *fixture) clones(t *testing.T) []string {
	t.Helper()
	entries, _ := os.ReadDir(filepath.Join(f.home, "orgs"))
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func TestConnectOrganization(t *testing.T) {
	t.Run("a clone with one record is kept under orgs and becomes active", func(t *testing.T) {
		f := newFixture(t)
		result, err := f.orchestrator().Workflows.ConnectOrganization("git@example:acme.git")
		if err != nil || result.Slug != "acme" || result.Name != "Acme" || !result.BecameActive {
			t.Fatalf("result %+v err %v", result, err)
		}
		if result.Clone != filepath.Join(f.home, "orgs", "0f8fad5b-d9cb-469f-a165-70867728950e") || f.state.state.Active != "acme" {
			t.Fatalf("clone %q active %q", result.Clone, f.state.state.Active)
		}
		if f.state.state.Clones["0f8fad5b-d9cb-469f-a165-70867728950e"] != result.Clone {
			t.Fatalf("clones %v", f.state.state.Clones)
		}
	})
	t.Run("a second connect of the same organisation is refused and leaves the first clone alone", func(t *testing.T) {
		f := newFixture(t)
		first, _ := f.orchestrator().Workflows.ConnectOrganization("x")
		marker := filepath.Join(first.Clone, "marker")
		_ = os.WriteFile(marker, []byte("x"), 0o644)
		_, err := f.orchestrator().Workflows.ConnectOrganization("x")
		if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "already connected") {
			t.Fatalf("err %v", err)
		}
		_, err = os.Stat(marker)
		if err != nil || len(f.clones(t)) != 1 || len(f.state.state.Organizations) != 1 {
			t.Fatalf("marker %v clones %v organisations %v", err, f.clones(t), f.state.state.Organizations)
		}
	})
	t.Run("a clone without a record is refused and removed", func(t *testing.T) {
		f := newFixture(t)
		f.repository = fakeRepository{}
		_, err := f.orchestrator().Workflows.ConnectOrganization("x")
		if !errors.Is(err, ErrRefused) || len(f.clones(t)) != 0 {
			t.Fatalf("err %v clones %v", err, f.clones(t))
		}
	})
	t.Run("a failed clone is not a refusal", func(t *testing.T) {
		f := newFixture(t)
		f.repository = fakeRepository{fail: true}
		_, err := f.orchestrator().Workflows.ConnectOrganization("x")
		if err == nil || errors.Is(err, ErrRefused) {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("a failure after registering compensates: the registration is forgotten and the clone removed", func(t *testing.T) {
		f := newFixture(t)
		// Register's save fails: the only completed step is the clone, which is undone.
		f.state.failSaves = 1
		_, err := f.orchestrator().Workflows.ConnectOrganization("x")
		if err == nil || errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "disk full") {
			t.Fatalf("err %v", err)
		}
		if len(f.clones(t)) != 0 || len(f.state.state.Organizations) != 0 {
			t.Fatalf("clones %v organisations %v", f.clones(t), f.state.state.Organizations)
		}
	})
	t.Run("without an identity every organisation operation refuses", func(t *testing.T) {
		f := newFixture(t)
		f.host = fakeHost{}
		orchestrator := f.orchestrator()
		_, err := orchestrator.Workflows.ConnectOrganization("x")
		_, _, listErr := orchestrator.Organizations.List()
		useErr := orchestrator.Organizations.Use("acme")
		_, disconnectErr := orchestrator.Workflows.DisconnectOrganization("acme")
		for _, candidate := range []error{err, listErr, useErr, disconnectErr} {
			if !errors.Is(candidate, ErrRefused) || !strings.Contains(candidate.Error(), "gh auth login") {
				t.Fatalf("err %v", candidate)
			}
		}
	})
}

func TestOrganizationsDirect(t *testing.T) {
	t.Run("use of an unknown slug is refused; list shows the active one", func(t *testing.T) {
		f := newFixture(t)
		_, _ = f.orchestrator().Workflows.ConnectOrganization("x")
		err := f.orchestrator().Organizations.Use("other")
		if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "not connected") {
			t.Fatalf("err %v", err)
		}
		connected, active, err := f.orchestrator().Organizations.List()
		if err != nil || len(connected) != 1 || active != "acme" {
			t.Fatalf("connected %v active %q err %v", connected, active, err)
		}
	})
}

func TestDisconnectOrganization(t *testing.T) {
	t.Run("forgets the organisation and removes its clone", func(t *testing.T) {
		f := newFixture(t)
		_, _ = f.orchestrator().Workflows.ConnectOrganization("x")
		result, err := f.orchestrator().Workflows.DisconnectOrganization("acme")
		if err != nil || result.Active != "" || len(f.state.state.Organizations) != 0 {
			t.Fatalf("result %+v err %v state %+v", result, err, f.state.state)
		}
		_, err = os.Stat(result.Clone)
		if err == nil {
			t.Fatalf("clone still there")
		}
	})
	t.Run("an unknown slug is refused", func(t *testing.T) {
		f := newFixture(t)
		_, err := f.orchestrator().Workflows.DisconnectOrganization("nobody")
		if !errors.Is(err, ErrRefused) {
			t.Fatalf("err %v", err)
		}
	})
}

func TestStatus(t *testing.T) {
	t.Run("a logged-out host is reported, not refused", func(t *testing.T) {
		f := newFixture(t)
		f.host = fakeHost{}
		report, err := f.orchestrator().Queries.Status()
		if err != nil || !strings.Contains(report.IdentityError, "gh auth login") || report.Identity.Login != "" {
			t.Fatalf("report %+v err %v", report, err)
		}
	})
	t.Run("placements are checked for their targets and agents are listed", func(t *testing.T) {
		f := newFixture(t)
		present := filepath.Join(t.TempDir(), "present")
		_ = os.WriteFile(present, []byte("x"), 0o644)
		f.state.state.Placements = []machine.Placement{{Path: "/a", Target: present}, {Path: "/b", Target: "/nowhere/gone"}}
		f.state.state.Projects = []machine.Project{{Organization: "id-1"}}
		report, err := f.orchestrator().Queries.Status()
		if err != nil || report.Missing != 1 || len(report.Placements) != 2 || !report.Placements[0].TargetExists || report.Placements[1].TargetExists {
			t.Fatalf("report %+v err %v", report, err)
		}
		if len(report.Agents) != 1 || report.Agents[0] != "fake-agent" || len(report.Projects) != 1 {
			t.Fatalf("agents %v projects %v", report.Agents, report.Projects)
		}
		f.agent.installed = false
		report, _ = f.orchestrator().Queries.Status()
		if len(report.Agents) != 0 {
			t.Fatalf("agents %v", report.Agents)
		}
	})
}

func TestReset(t *testing.T) {
	t.Run("the plan lists what the state holds", func(t *testing.T) {
		f := newFixture(t)
		f.state.state.Placements = []machine.Placement{{Path: "/a"}}
		f.state.state.Organizations = []machine.OrganizationRef{{Slug: "acme"}}
		plan, err := f.orchestrator().Queries.PlanReset(true)
		if err != nil || len(plan.Placements) != 1 || len(plan.Organizations) != 1 || !plan.All {
			t.Fatalf("plan %+v err %v", plan, err)
		}
		if plan.OrgsFolder != filepath.Join(f.home, "orgs") || plan.ImportsFolder != filepath.Join(f.home, "imports") {
			t.Fatalf("plan %+v", plan)
		}
	})
	t.Run("reset unplaces through the agent, keeps what was skipped, and with all forgets the machine", func(t *testing.T) {
		f := newFixture(t)
		clone := filepath.Join(f.home, "orgs", "id-1")
		_ = os.MkdirAll(clone, 0o755)
		_ = os.MkdirAll(filepath.Join(f.home, "imports"), 0o755)
		f.state.state.Placements = []machine.Placement{{Path: "/a"}, {Path: "/foreign"}}
		f.state.state.Organizations = []machine.OrganizationRef{{ID: "id-1", Slug: "acme"}}
		f.state.state.Clones = map[string]string{"id-1": clone}
		f.state.state.Active = "acme"
		f.state.state.Projects = []machine.Project{{Organization: "id-1"}}
		f.state.state.Imports = []machine.Import{{}}
		plan, _ := f.orchestrator().Queries.PlanReset(true)
		result, err := f.orchestrator().Workflows.ResetMachine(plan)
		if err != nil || result.Kept != 1 || len(result.RemovedClones) != 1 || len(f.agent.unplaced) != 2 {
			t.Fatalf("result %+v err %v unplaced %v", result, err, f.agent.unplaced)
		}
		state := f.state.state
		if len(state.Placements) != 1 || state.Placements[0].Path != "/foreign" || state.Active != "" || len(state.Organizations) != 0 || len(state.Projects) != 0 || len(state.Imports) != 0 {
			t.Fatalf("state %+v", state)
		}
		for _, gone := range []string{clone, filepath.Join(f.home, "imports")} {
			_, err = os.Stat(gone)
			if err == nil {
				t.Fatalf("%s still there", gone)
			}
		}
	})
	t.Run("without all the organisations stay", func(t *testing.T) {
		f := newFixture(t)
		f.state.state.Organizations = []machine.OrganizationRef{{ID: "id-1", Slug: "acme"}}
		f.state.state.Active = "acme"
		plan, _ := f.orchestrator().Queries.PlanReset(false)
		_, err := f.orchestrator().Workflows.ResetMachine(plan)
		if err != nil || len(f.state.state.Organizations) != 1 || f.state.state.Active != "acme" {
			t.Fatalf("err %v state %+v", err, f.state.state)
		}
	})
}
