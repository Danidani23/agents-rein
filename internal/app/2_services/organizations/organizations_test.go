package organizations

import (
	"errors"
	"testing"
	"time"

	"github.com/Danidani23/agents-rein/internal/adapters/fake"
	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
)

func TestService(t *testing.T) {
	acme := machine.OrganizationRef{ID: "id-1", Slug: "acme", Remote: "r1", Host: "github"}
	other := machine.OrganizationRef{ID: "id-2", Slug: "other", Remote: "r2", Host: "github"}

	t.Run("the first registered becomes active and is stamped with the time, a slug registers once", func(t *testing.T) {
		state := fake.NewState()
		connectedAt := time.Date(2026, 10, 19, 9, 30, 0, 0, time.UTC)
		service := Service{State: state, Clock: func() time.Time { return connectedAt }}
		becameActive, err := service.Register(acme, "/clones/1")
		if err != nil || !becameActive || !state.Current.Organizations[0].ConnectedAt.Equal(connectedAt) {
			t.Fatalf("active %v err %v organisations %+v", becameActive, err, state.Current.Organizations)
		}
		becameActive, err = service.Register(other, "/clones/2")
		if err != nil || becameActive {
			t.Fatalf("active %v err %v", becameActive, err)
		}
		_, err = service.Register(acme, "/clones/3")
		var already ErrAlreadyConnected
		if !errors.As(err, &already) || !errors.Is(err, refusal.ErrRefused) || already.Slug != "acme" || state.Current.OrgClonePaths["id-1"] != "/clones/1" {
			t.Fatalf("err %v clones %v", err, state.Current.OrgClonePaths)
		}
	})
	t.Run("use switches the active one and refuses an unknown slug", func(t *testing.T) {
		state := fake.NewState()
		service := Service{State: state}
		_, _ = service.Register(acme, "/clones/1")
		_, _ = service.Register(other, "/clones/2")
		err := service.Use("other")
		if err != nil || state.Current.Active != "other" {
			t.Fatalf("err %v active %q", err, state.Current.Active)
		}
		var notConnected ErrNotConnected
		if err = service.Use("nobody"); !errors.As(err, &notConnected) || !errors.Is(err, refusal.ErrRefused) {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("forget removes the organisation, its clone path and its projects, and restore puts them back", func(t *testing.T) {
		state := fake.NewState()
		service := Service{State: state}
		_, _ = service.Register(acme, "/clones/1")
		_, _ = service.Register(other, "/clones/2")
		state.Current.ProjectClones = []machine.ProjectClone{{Organization: "id-1", ClonePath: "/p1"}, {Organization: "id-2", ClonePath: "/p2"}}
		forgotten, err := service.Forget("acme")
		if err != nil || forgotten.Organization.ID != acme.ID || forgotten.OrgClonePath != "/clones/1" || len(forgotten.RemovedProjectClones) != 1 || forgotten.ActiveAfter != "other" {
			t.Fatalf("forgotten %+v err %v", forgotten, err)
		}
		if len(state.Current.Organizations) != 1 || len(state.Current.ProjectClones) != 1 || state.Current.OrgClonePaths["id-1"] != "" {
			t.Fatalf("state %+v", state.Current)
		}
		err = service.Restore(forgotten)
		projects, _ := service.ProjectClones()
		if err != nil || len(state.Current.Organizations) != 2 || len(projects) != 2 || state.Current.OrgClonePaths["id-1"] != "/clones/1" || state.Current.Active != "other" {
			t.Fatalf("err %v state %+v", err, state.Current)
		}
	})
	t.Run("forget all empties the section", func(t *testing.T) {
		state := fake.NewState()
		service := Service{State: state}
		_, _ = service.Register(acme, "/clones/1")
		removed, orgClonePaths, err := service.ForgetAll()
		if err != nil || len(removed) != 1 || orgClonePaths["id-1"] != "/clones/1" || state.Current.Active != "" || len(state.Current.Organizations) != 0 {
			t.Fatalf("removed %v clones %v err %v state %+v", removed, orgClonePaths, err, state.Current)
		}
	})
}
