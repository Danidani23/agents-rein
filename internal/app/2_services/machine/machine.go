// Package machine is the model of what rein remembers about one machine: the
// connected organisations, the active one, the clones, the project
// registrations and the placements it made. It is a cache (D9), rebuildable
// from the clones and from sync, and it never holds an identity or a secret.
package machine

// OrganizationRef is a connected organisation. Host is the forge named in
// its record, mirrored here so the right adapter is picked without reading
// the clone.
type OrganizationRef struct {
	ID     string `yaml:"id"`
	Slug   string `yaml:"slug"`
	Remote string `yaml:"remote"`
	Host   string `yaml:"host,omitempty"`
}

// Placement is one link or copy rein made in an agent's home. Owner is the
// command that made it and Source what it placed it from; a command may
// replace only its own placements from the same source.
type Placement struct {
	Path   string `yaml:"path"`
	Target string `yaml:"target"`
	Mode   string `yaml:"mode"`
	Owner  string `yaml:"owner,omitempty"`
	Source string `yaml:"source,omitempty"`
}

// Project is a project repository registered on this machine.
type Project struct {
	Organization string `yaml:"organization"`
	Address      string `yaml:"address"`
	ClonePath    string `yaml:"clone_path"`
	Fingerprint  string `yaml:"fingerprint,omitempty"`
	LastSync     string `yaml:"last_sync,omitempty"`
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

// State is everything rein remembers about this machine.
type State struct {
	Organizations []OrganizationRef `yaml:"organizations"`
	Active        string            `yaml:"active,omitempty"`
	Clones        map[string]string `yaml:"clones"`
	Projects      []Project         `yaml:"projects"`
	Placements    []Placement       `yaml:"placements"`
	Imports       []Import          `yaml:"imports,omitempty"`
}

// Empty is the state of a machine rein has never touched.
func Empty() State {
	return State{Clones: map[string]string{}}
}

// Organization finds a connected organisation by slug.
func (state State) Organization(slug string) (ref OrganizationRef, found bool) {
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
