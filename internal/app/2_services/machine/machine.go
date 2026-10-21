// Package machine is the model of what rein remembers about one machine: the
// connected organisations and since when, the active one, where their clones
// are, the project registrations and the placements it made. Every record
// carries the time it was written (D20). It never holds an identity or a
// secret.
package machine

import "time"

// OrganizationRef is a connected organisation. Host is the forge named in
// its record, mirrored here so the right adapter is picked without reading
// the clone.
type OrganizationRef struct {
	ID     string `yaml:"id"`
	Slug   string `yaml:"slug"`
	Remote string `yaml:"remote"`
	Host   string `yaml:"host,omitempty"`
	// Login is the host account whose token acts on this organisation; Owner
	// and Repository name the org repository on the host (D25).
	Login       string    `yaml:"login,omitempty"`
	Owner       string    `yaml:"owner,omitempty"`
	Repository  string    `yaml:"repository,omitempty"`
	ConnectedAt time.Time `yaml:"connected_at,omitempty"`
}

// Placement is one link or copy rein made in an agent's home and, from sync
// on, in a project clone. Owner is the command that made it and Source what
// it placed it from; a command may replace only its own placements from the
// same source. This is the agent-home case only; P2.15 grows it into the
// ledger's record with the agent, the scope, the time and what was there
// before.
type Placement struct {
	Path   string `yaml:"path"`
	Target string `yaml:"target"`
	Mode   string `yaml:"mode"`
	Owner  string `yaml:"owner,omitempty"`
	Source string `yaml:"source,omitempty"`
}

// ProjectClone is a project clone initialised on this machine: ClonePath is
// the project's folder, the repository's root or, in a monorepo, the
// project's path under it; Fingerprint the root commits joined with commas.
type ProjectClone struct {
	Organization  string    `yaml:"organization"`
	Address       string    `yaml:"address"`
	ClonePath     string    `yaml:"clone_path"`
	Fingerprint   string    `yaml:"fingerprint,omitempty"`
	LastSync      string    `yaml:"last_sync,omitempty"`
	InitializedAt time.Time `yaml:"initialized_at,omitempty"`
}

// Import is a level unpacked on this machine by the import of 2026-10-15,
// kept so reset can remove what it left.
type Import struct {
	Hash     string `yaml:"hash"`
	Path     string `yaml:"path"`
	FromUser string `yaml:"from_user,omitempty"`
	Source   string `yaml:"source,omitempty"`
	Imported string `yaml:"imported"`
}

// StateVersion is the version of the state file this build writes and reads;
// a file from a newer rein is refused (D19).
const StateVersion = 1

// State is everything rein remembers about this machine.
type State struct {
	Version int `yaml:"version"`
	// Logins are the host accounts logged in on this machine; their tokens
	// are in the keychain (D25).
	Logins        []string          `yaml:"logins,omitempty"`
	Organizations []OrganizationRef `yaml:"organizations"`
	Active        string            `yaml:"active,omitempty"`
	OrgClonePaths map[string]string `yaml:"org_clone_paths"`
	ProjectClones []ProjectClone    `yaml:"project_clones"`
	Placements    []Placement       `yaml:"placements"`
	Imports       []Import          `yaml:"imports,omitempty"`
}

// Empty is the state of a machine rein has never touched.
func Empty() State {
	return State{Version: StateVersion, OrgClonePaths: map[string]string{}}
}

// Organization finds a connected organisation by slug.
func (state State) Organization(slug string) (organization OrganizationRef, found bool) {
	for _, candidate := range state.Organizations {
		if candidate.Slug == slug {
			return candidate, true
		}
	}
	return OrganizationRef{}, false
}

// OwnedBy maps path to target for the placements of one owner from one source.
func (state State) OwnedBy(owner string, source string) map[string]string {
	known := map[string]string{}
	for _, placed := range state.Placements {
		if placed.Owner == owner && placed.Source == source {
			known[placed.Path] = placed.Target
		}
	}
	return known
}
