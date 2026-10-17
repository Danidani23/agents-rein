package organizations

import (
	"errors"
	"testing"

	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
)

type memoryState struct {
	state machine.State
}

func (memory *memoryState) Load() (machine.State, error) { return memory.state, nil }
func (memory *memoryState) Save(state machine.State) error {
	memory.state = state
	return nil
}

func TestService(t *testing.T) {
	acme := machine.OrganizationRef{ID: "id-1", Slug: "acme", Remote: "r1", Host: "github"}
	other := machine.OrganizationRef{ID: "id-2", Slug: "other", Remote: "r2", Host: "github"}

	t.Run("the first registered becomes active, a slug registers once", func(t *testing.T) {
		state := &memoryState{state: machine.Empty()}
		service := Service{State: state}
		becameActive, err := service.Register(acme, "/clones/1")
		if err != nil || !becameActive {
			t.Fatalf("active %v err %v", becameActive, err)
		}
		becameActive, err = service.Register(other, "/clones/2")
		if err != nil || becameActive {
			t.Fatalf("active %v err %v", becameActive, err)
		}
		_, err = service.Register(acme, "/clones/3")
		var already ErrAlreadyConnected
		if !errors.As(err, &already) || already.Slug != "acme" || state.state.Clones["id-1"] != "/clones/1" {
			t.Fatalf("err %v clones %v", err, state.state.Clones)
		}
	})
	t.Run("use switches the active one and refuses an unknown slug", func(t *testing.T) {
		state := &memoryState{state: machine.Empty()}
		service := Service{State: state}
		_, _ = service.Register(acme, "/clones/1")
		_, _ = service.Register(other, "/clones/2")
		err := service.Use("other")
		if err != nil || state.state.Active != "other" {
			t.Fatalf("err %v active %q", err, state.state.Active)
		}
		var notConnected ErrNotConnected
		if err = service.Use("nobody"); !errors.As(err, &notConnected) {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("forget removes the organisation, its clone path and its projects, and restore puts them back", func(t *testing.T) {
		state := &memoryState{state: machine.Empty()}
		service := Service{State: state}
		_, _ = service.Register(acme, "/clones/1")
		_, _ = service.Register(other, "/clones/2")
		state.state.Projects = []machine.Project{{Organization: "id-1", ClonePath: "/p1"}, {Organization: "id-2", ClonePath: "/p2"}}
		forgotten, err := service.Forget("acme")
		if err != nil || forgotten.Ref != acme || forgotten.Clone != "/clones/1" || len(forgotten.RemovedProjects) != 1 || forgotten.Active != "other" {
			t.Fatalf("forgotten %+v err %v", forgotten, err)
		}
		if len(state.state.Organizations) != 1 || len(state.state.Projects) != 1 || state.state.Clones["id-1"] != "" {
			t.Fatalf("state %+v", state.state)
		}
		err = service.Restore(forgotten)
		projects, _ := service.Projects()
		if err != nil || len(state.state.Organizations) != 2 || len(projects) != 2 || state.state.Clones["id-1"] != "/clones/1" || state.state.Active != "other" {
			t.Fatalf("err %v state %+v", err, state.state)
		}
	})
	t.Run("forget all empties the section", func(t *testing.T) {
		state := &memoryState{state: machine.Empty()}
		service := Service{State: state}
		_, _ = service.Register(acme, "/clones/1")
		removed, clones, err := service.ForgetAll()
		if err != nil || len(removed) != 1 || clones["id-1"] != "/clones/1" || state.state.Active != "" || len(state.state.Organizations) != 0 {
			t.Fatalf("removed %v clones %v err %v state %+v", removed, clones, err, state.state)
		}
	})
}
