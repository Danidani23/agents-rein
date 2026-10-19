package orchestration

import "github.com/Danidani23/agents-rein/internal/app/2_services/organizations"

// DisconnectResult is what DisconnectOrganization did, and which
// organisation is active afterwards.
type DisconnectResult struct {
	OrgClonePath    string
	RemovedProjects int
	ActiveAfter     string
}

// DisconnectOrganization forgets an organisation and removes its clone; the
// remote is never touched. If the clone cannot be removed the registration
// is restored.
func (orchestrator Orchestrator) DisconnectOrganization(slug string) (DisconnectResult, error) {
	services := orchestrator.Services
	var forgotten organizations.Forgotten
	var err error
	err = runSteps([]Step{
		{
			Name: "forget",
			Do: func() error {
				forgotten, err = services.Organizations.Forget(slug)
				return err
			},
			Undo: func() error { return services.Organizations.Restore(forgotten) },
		},
		{
			Name: "remove clone",
			Do:   func() error { return services.Clones.Remove(forgotten.OrgClonePath) },
		},
	})
	if err != nil {
		return DisconnectResult{}, err
	}
	return DisconnectResult{OrgClonePath: forgotten.OrgClonePath, RemovedProjects: len(forgotten.RemovedProjects), ActiveAfter: forgotten.ActiveAfter}, nil
}
