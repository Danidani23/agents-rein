package machine

import "testing"

func TestState(t *testing.T) {
	t.Run("Empty has a usable clones map", func(t *testing.T) {
		if Empty().Clones == nil {
			t.Fatalf("nil clones")
		}
	})
	t.Run("Organization finds by slug", func(t *testing.T) {
		state := State{Organizations: []OrganizationRef{{ID: "1", Slug: "dani"}}}
		_, found := state.Organization("dani")
		_, other := state.Organization("other")
		if !found || other {
			t.Fatalf("found %v other %v", found, other)
		}
	})
	t.Run("OwnedBy filters by owner and source", func(t *testing.T) {
		state := State{Placements: []Placement{{Path: "/a", Target: "/t", Owner: "import", Source: "/x"}, {Path: "/b", Target: "/u", Owner: "import", Source: "/y"}}}
		if len(state.OwnedBy("import", "/x")) != 1 || len(state.OwnedBy("sync", "/x")) != 0 {
			t.Fatalf("owned %v", state.OwnedBy("import", "/x"))
		}
	})
}
