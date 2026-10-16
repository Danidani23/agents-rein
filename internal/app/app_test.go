package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Danidani23/agents-rein/internal/domain/machine"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// The fakes: each port with the least behaviour a use case needs.

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

type memoryState struct {
	state machine.State
}

func (memory *memoryState) Load() (machine.State, error) { return memory.state, nil }
func (memory *memoryState) Save(state machine.State) error {
	memory.state = state
	return nil
}

const validRecord = "id: 0f8fad5b-d9cb-469f-a165-70867728950e\nslug: acme\nname: Acme\n"

func dependencies(t *testing.T) (Dependencies, *memoryState, *fakeAgent) {
	t.Helper()
	state := &memoryState{state: machine.Empty()}
	agent := &fakeAgent{installed: true}
	return Dependencies{
		Home:       t.TempDir(),
		Host:       fakeHost{login: "Danidani23"},
		Repository: fakeRepository{record: validRecord},
		Agents:     []ports.Agent{agent},
		State:      state,
	}, state, agent
}

func TestOrgConnect(t *testing.T) {
	t.Run("a clone with one record is kept under orgs and becomes active", func(t *testing.T) {
		deps, state, _ := dependencies(t)
		result, err := OrgConnect(deps, "git@example:acme.git")
		if err != nil || result.Slug != "acme" || result.Name != "Acme" || !result.BecameActive {
			t.Fatalf("result %+v err %v", result, err)
		}
		if result.Clone != filepath.Join(deps.Home, "orgs", "0f8fad5b-d9cb-469f-a165-70867728950e") || state.state.Active != "acme" {
			t.Fatalf("clone %q active %q", result.Clone, state.state.Active)
		}
	})
	t.Run("a second connect of the same organisation is refused", func(t *testing.T) {
		deps, _, _ := dependencies(t)
		_, _ = OrgConnect(deps, "x")
		_, err := OrgConnect(deps, "x")
		if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "already connected") {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("a clone without a record is refused and removed", func(t *testing.T) {
		deps, _, _ := dependencies(t)
		deps.Repository = fakeRepository{}
		_, err := OrgConnect(deps, "x")
		if !errors.Is(err, ErrRefused) {
			t.Fatalf("err %v", err)
		}
		entries, _ := os.ReadDir(filepath.Join(deps.Home, "orgs"))
		if len(entries) != 0 {
			t.Fatalf("leftover clones: %v", entries)
		}
	})
	t.Run("a failed clone is not a refusal", func(t *testing.T) {
		deps, _, _ := dependencies(t)
		deps.Repository = fakeRepository{fail: true}
		_, err := OrgConnect(deps, "x")
		if err == nil || errors.Is(err, ErrRefused) {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("without an identity every org use case refuses", func(t *testing.T) {
		deps, _, _ := dependencies(t)
		deps.Host = fakeHost{}
		_, err := OrgConnect(deps, "x")
		_, _, listErr := OrgList(deps)
		useErr := OrgUse(deps, "acme")
		_, disconnectErr := OrgDisconnect(deps, "acme")
		for _, candidate := range []error{err, listErr, useErr, disconnectErr} {
			if !errors.Is(candidate, ErrRefused) || !strings.Contains(candidate.Error(), "gh auth login") {
				t.Fatalf("err %v", candidate)
			}
		}
	})
	t.Run("use and disconnect work on the state", func(t *testing.T) {
		deps, state, _ := dependencies(t)
		_, _ = OrgConnect(deps, "x")
		err := OrgUse(deps, "other")
		if !errors.Is(err, ErrRefused) {
			t.Fatalf("use of an unknown slug: %v", err)
		}
		result, err := OrgDisconnect(deps, "acme")
		if err != nil || result.Active != "" || len(state.state.Organizations) != 0 {
			t.Fatalf("result %+v err %v state %+v", result, err, state.state)
		}
		_, err = os.Stat(result.Clone)
		if err == nil {
			t.Fatalf("clone still there")
		}
	})
}

func TestStatus(t *testing.T) {
	t.Run("a logged-out host is reported, not refused", func(t *testing.T) {
		deps, _, _ := dependencies(t)
		deps.Host = fakeHost{}
		report, err := Status(deps)
		if err != nil || !strings.Contains(report.IdentityError, "gh auth login") || report.Identity.Login != "" {
			t.Fatalf("report %+v err %v", report, err)
		}
	})
	t.Run("placements are checked for their targets and agents are listed", func(t *testing.T) {
		deps, state, agent := dependencies(t)
		present := filepath.Join(t.TempDir(), "present")
		_ = os.WriteFile(present, []byte("x"), 0o644)
		state.state.Placements = []machine.Placement{{Path: "/a", Target: present}, {Path: "/b", Target: "/nowhere/gone"}}
		report, err := Status(deps)
		if err != nil || report.Missing != 1 || len(report.Placements) != 2 || !report.Placements[0].TargetExists || report.Placements[1].TargetExists {
			t.Fatalf("report %+v err %v", report, err)
		}
		if len(report.Agents) != 1 || report.Agents[0] != "fake-agent" {
			t.Fatalf("agents %v", report.Agents)
		}
		agent.installed = false
		report, _ = Status(deps)
		if len(report.Agents) != 0 {
			t.Fatalf("agents %v", report.Agents)
		}
	})
}

func TestReset(t *testing.T) {
	t.Run("the plan lists what the state holds", func(t *testing.T) {
		deps, state, _ := dependencies(t)
		state.state.Placements = []machine.Placement{{Path: "/a"}}
		state.state.Organizations = []machine.OrganizationRef{{Slug: "acme"}}
		plan, err := PlanReset(deps, true)
		if err != nil || len(plan.Placements) != 1 || len(plan.Organizations) != 1 || !plan.All {
			t.Fatalf("plan %+v err %v", plan, err)
		}
	})
	t.Run("apply unplaces through the agent, keeps what was skipped, and with all forgets the machine", func(t *testing.T) {
		deps, state, agent := dependencies(t)
		clone := filepath.Join(deps.Home, "orgs", "id-1")
		_ = os.MkdirAll(clone, 0o755)
		state.state.Placements = []machine.Placement{{Path: "/a"}, {Path: "/foreign"}}
		state.state.Organizations = []machine.OrganizationRef{{ID: "id-1", Slug: "acme"}}
		state.state.Clones = map[string]string{"id-1": clone}
		state.state.Active = "acme"
		state.state.Projects = []machine.Project{{Organization: "id-1"}}
		plan, _ := PlanReset(deps, true)
		result, err := ApplyReset(deps, plan)
		if err != nil || result.Kept != 1 || len(result.RemovedClones) != 1 || len(agent.unplaced) != 2 {
			t.Fatalf("result %+v err %v unplaced %v", result, err, agent.unplaced)
		}
		if len(state.state.Placements) != 1 || state.state.Placements[0].Path != "/foreign" || state.state.Active != "" || len(state.state.Organizations) != 0 || len(state.state.Projects) != 0 {
			t.Fatalf("state %+v", state.state)
		}
		_, err = os.Stat(clone)
		if err == nil {
			t.Fatalf("clone still there")
		}
	})
}
