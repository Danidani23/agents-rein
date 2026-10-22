package orchestration

import (
	"fmt"
	"path"
	"strings"

	"github.com/Danidani23/agents-rein/internal/app/2_services/ledger"
	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
	"github.com/Danidani23/agents-rein/internal/app/2_services/proposalfiles"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
	"github.com/Danidani23/agents-rein/internal/app/2_services/scaffold"
	"github.com/Danidani23/agents-rein/internal/app/2_services/workingcopy"
)

// InitializeProjectRequest is what the person decided at init. The clone
// binds to the record that already carries its fingerprint, to the record
// named by Organization and Project, or to the one Create makes.
type InitializeProjectRequest struct {
	Directory    string
	Organization string
	Project      string
	Create       *CreateProjectRequest
	NoCommit     bool
}

// MoveOut is what init took out of the clone for one agent.
type MoveOut struct {
	Agent        string
	SnapshotPath string
	Paths        []string
	files        map[string]string
	tracked      map[string]bool
}

// InitializeResult is what InitializeProject did.
type InitializeResult struct {
	Root          string
	Path          string
	Folder        string
	Address       string
	Fingerprint   []string
	Created       bool
	Bound         bool
	MovedOut      []MoveOut
	IgnoreChanged bool
	Commit        string
	// CommitError is why the commit did not happen; the paths stay staged.
	CommitError string
}

const moveOutCommitMessage = "move agent settings out to rein (rein init)"

// InitializeProject binds a clone to its project record, moves the agents'
// settings out of it into snapshots, records the removals in the ledger,
// adds the marked block to .gitignore, registers the clone on this machine
// and commits exactly the paths it touched (D14). A failure before the
// commit puts everything back; the commit itself never fails the workflow,
// it is reported.
func (orchestrator Orchestrator) InitializeProject(request InitializeProjectRequest) (InitializeResult, error) {
	services := orchestrator.Services
	location, err := services.WorkingCopy.Locate(request.Directory)
	if err != nil {
		return InitializeResult{}, refusal.New(fmt.Sprintf("%s is not inside a git repository with a commit: %v", request.Directory, err))
	}
	result := InitializeResult{Root: location.Root, Path: location.Path, Folder: location.Folder, Fingerprint: location.Fingerprint}
	registered, err := services.Organizations.ProjectClones()
	if err != nil {
		return InitializeResult{}, err
	}
	for _, project := range registered {
		if project.ClonePath == location.Folder {
			return InitializeResult{}, refusal.New(fmt.Sprintf("%s is already initialised as %s; run rein sync", location.Folder, project.Address))
		}
	}
	organization, orgClonePath, err := resolveOrganization(services, request.Organization)
	if err != nil {
		return InitializeResult{}, err
	}
	var registration machine.ProjectClone
	steps := []Step{
		{
			Name: "binding the project record",
			Do: func() error {
				var err error
				result.Address, result.Created, result.Bound, err = bindProject(orchestrator, request, organization, orgClonePath, location)
				return err
			},
		},
	}
	for _, agent := range services.Snapshots.Agents {
		agent := agent
		moveOut := MoveOut{Agent: agent.Name()}
		steps = append(steps,
			Step{
				Name: "snapshotting the files of " + agent.Name(),
				Do: func() error {
					moveOut.Paths = services.WorkingCopy.Present(location.Folder, agent.ProjectHarnessPaths())
					if len(moveOut.Paths) == 0 {
						return nil
					}
					var fromRoot []string
					for _, removed := range moveOut.Paths {
						fromRoot = append(fromRoot, path.Join(location.Path, removed))
					}
					tracked, err := services.WorkingCopy.TrackedAmong(location.Root, fromRoot)
					if err != nil {
						return err
					}
					moveOut.tracked = map[string]bool{}
					for _, removed := range moveOut.Paths {
						moveOut.tracked[removed] = tracked[path.Join(location.Path, removed)]
					}
					moveOut.SnapshotPath, moveOut.files, err = services.Snapshots.Take(agent.Name(), path.Base(result.Address), location.Folder, agent.ProjectHarnessPaths())
					return err
				},
				Undo: func() error { return services.Snapshots.Remove(moveOut.SnapshotPath) },
			},
			Step{
				Name: "moving the files of " + agent.Name() + " out",
				Do: func() error {
					if len(moveOut.Paths) == 0 {
						return nil
					}
					err := services.WorkingCopy.Remove(location.Folder, moveOut.Paths)
					if err != nil {
						return err
					}
					result.MovedOut = append(result.MovedOut, moveOut)
					return nil
				},
				Undo: func() error { return services.WorkingCopy.Restore(location.Folder, moveOut.files) },
			},
			Step{
				Name: "recording the removals of " + agent.Name(),
				Do: func() error {
					if len(moveOut.Paths) == 0 {
						return nil
					}
					var removals []ledger.Removal
					for _, removed := range moveOut.Paths {
						removals = append(removals, ledger.Removal{Path: path.Join(location.Folder, removed), Scope: ledger.ScopeProject, Project: result.Address, Snapshot: moveOut.SnapshotPath, Tracked: moveOut.tracked[removed]})
					}
					return services.Ledger.RecordRemovals(agent.Name(), removals)
				},
				Undo: func() error { return services.Ledger.ForgetSnapshot(moveOut.SnapshotPath) },
			},
		)
	}
	steps = append(steps,
		Step{
			Name: "writing the .gitignore block",
			Do: func() error {
				var err error
				result.IgnoreChanged, err = services.WorkingCopy.WriteIgnoreBlock(location.Folder, ignoreEntries(services))
				return err
			},
			Undo: func() error {
				if !result.IgnoreChanged {
					return nil
				}
				return services.WorkingCopy.RemoveIgnoreBlock(location.Folder)
			},
		},
		Step{
			Name: "registering the clone",
			Do: func() error {
				registration = machine.ProjectClone{Organization: organization.ID, Address: result.Address, ClonePath: location.Folder, Fingerprint: strings.Join(location.Fingerprint, ",")}
				return services.Organizations.RegisterProjectClone(registration)
			},
			Undo: func() error { return services.Organizations.UnregisterProjectClone(location.Folder) },
		},
	)
	err = runSteps(services.Progress, steps)
	if err != nil {
		return InitializeResult{}, err
	}
	if request.NoCommit {
		return result, nil
	}
	var removedPaths []string
	for _, moveOut := range result.MovedOut {
		for _, removed := range moveOut.Paths {
			removedPaths = append(removedPaths, path.Join(location.Path, removed))
		}
	}
	var extraPaths []string
	if result.IgnoreChanged {
		extraPaths = append(extraPaths, path.Join(location.Path, ".gitignore"))
	}
	hash, committed, err := services.WorkingCopy.Commit(location.Root, removedPaths, extraPaths, moveOutCommitMessage)
	switch {
	case err != nil:
		result.CommitError = err.Error()
	case committed:
		result.Commit = hash
	}
	return result, nil
}

// bindProject makes sure a project record carries the clone's fingerprint
// and path: it finds the one that already does, creates one, or writes the
// fingerprint into the named one and publishes it.
func bindProject(orchestrator Orchestrator, request InitializeProjectRequest, organization machine.OrganizationRef, orgClonePath string, location workingcopy.Location) (address string, created bool, bound bool, err error) {
	services := orchestrator.Services
	organizationSlug := organization.Slug
	if request.Create != nil {
		create := *request.Create
		create.Organization = request.Organization
		create.Path = location.Path
		create.Fingerprint = location.Fingerprint
		result, err := orchestrator.CreateProject(create)
		if err != nil {
			return "", false, false, err
		}
		return projectAddress(organizationSlug, result.Slug), true, true, nil
	}
	err = services.Records.Pull(accessOf(organization), orgClonePath)
	if err != nil {
		return "", false, false, err
	}
	tree, err := services.Records.Tree(orgClonePath)
	if err != nil {
		return "", false, false, err
	}
	if request.Project == "" {
		for _, slug := range sortedKeys(tree.Projects) {
			project := tree.Projects[slug]
			if sameFingerprint(project.Fingerprint, location.Fingerprint) && project.Path == location.Path {
				return projectAddress(organizationSlug, slug), false, false, nil
			}
		}
		return "", false, false, refusal.New(fmt.Sprintf("no project of %s carries this repository's fingerprint; name one with --project or create it", organizationSlug))
	}
	project, found := projectRecordOf(tree, request.Project)
	if !found {
		return "", false, false, refusal.New(fmt.Sprintf("project %q does not exist in %s; projects are %s", request.Project, organizationSlug, strings.Join(sortedKeys(tree.Projects), ", ")))
	}
	if sameFingerprint(project.Fingerprint, location.Fingerprint) && project.Path == location.Path {
		return projectAddress(organizationSlug, project.Slug), false, false, nil
	}
	if len(project.Fingerprint) > 0 {
		return "", false, false, refusal.New(fmt.Sprintf("project %q is bound to another repository; create a new project for this one", request.Project))
	}
	project.Fingerprint = location.Fingerprint
	project.Path = location.Path
	relativePath, content, err := scaffold.ProjectRecord(project)
	if err != nil {
		return "", false, false, err
	}
	err = services.Records.Rewrite(orgClonePath, relativePath, content)
	if err != nil {
		return "", false, false, err
	}
	err = services.Records.Publish(accessOf(organization), orgClonePath, []string{relativePath}, fmt.Sprintf("project %s bound to its repository (rein init)", project.Slug))
	if err != nil {
		discardErr := services.Records.Discard(orgClonePath)
		if discardErr != nil {
			return "", false, false, &CompensationError{Cause: err, Failures: []string{"discard: " + discardErr.Error()}}
		}
		return "", false, false, err
	}
	return projectAddress(organizationSlug, project.Slug), false, true, nil
}

// ignoreEntries are the project harness paths and the personal paths of
// every agent, once each.
func ignoreEntries(services Services) []string {
	entries := []string{proposalfiles.CloneFolder + "/"}
	seen := map[string]bool{}
	for _, agent := range services.Snapshots.Agents {
		for _, harnessPath := range append(append([]string{}, agent.ProjectHarnessPaths()...), agent.PersonalPaths()...) {
			if !seen[harnessPath] {
				seen[harnessPath] = true
				entries = append(entries, harnessPath)
			}
		}
	}
	return entries
}
