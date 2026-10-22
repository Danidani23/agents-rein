package orchestration

import (
	"fmt"
	"path"
	"strings"

	"github.com/Danidani23/agents-rein/internal/app/2_services/distill"
	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
	"github.com/Danidani23/agents-rein/internal/app/2_services/snapshots"
)

// DistillRequest says which harness to distill: the snapshot file named,
// the latest snapshots of the agents' homes for the user's level, or the
// latest snapshots of a project, found from Directory or named by Project
// in Organization.
type DistillRequest struct {
	Directory    string
	Organization string
	Project      string
	Home         bool
	SnapshotPath string
	// Judge is "agent-cli" or empty for the wired backend, "none" to skip
	// the model pass.
	Judge string
	// DryRun stops before asking the judge and before writing anything,
	// with the brief in the result.
	DryRun bool
}

// DistillResult is what DistillHarness did.
type DistillResult struct {
	Level      string
	Folder     string
	Report     distill.Report
	Candidates []distill.Candidate
	Brief      string
}

// DistillHarness turns the harness a snapshot holds into candidate
// declarations at its level, compares them with the organisation's
// declarations, and writes them with the report under ~/.rein/distill/ for
// propose. It never touches the org clone.
func (orchestrator Orchestrator) DistillHarness(request DistillRequest) (DistillResult, error) {
	services := orchestrator.Services
	level, organization, stored, err := distillSources(orchestrator, request)
	if err != nil {
		return DistillResult{}, err
	}
	if len(stored) == 0 {
		return DistillResult{}, refusal.New("no snapshot to distill: init a project, or connect an organisation, so rein snapshots the harness first")
	}
	_, orgClonePath, err := services.Organizations.Lookup(organization.Slug)
	if err != nil {
		return DistillResult{}, err
	}
	report := distill.Report{Version: 1, Level: level}
	result := DistillResult{Level: level}
	var candidates []distill.Candidate
	var corpus distill.Corpus
	judging := request.Judge != "none"
	steps := []Step{
		{
			Name: "reading the snapshot",
			Do: func() error {
				for _, snapshot := range stored {
					report.Snapshots = append(report.Snapshots, snapshot.Path)
					if snapshot.Agent != "claude-code" {
						report.Skipped = append(report.Skipped, fmt.Sprintf("%s: distill knows no harness of %s yet", snapshot.Path, snapshot.Agent))
						continue
					}
					converted, skipped, err := distill.ConvertClaudeCode(snapshot.Files)
					if err != nil {
						return err
					}
					candidates = append(candidates, converted...)
					report.Skipped = append(report.Skipped, skipped...)
				}
				result.Candidates = candidates
				return nil
			},
		},
		{
			Name: "comparing with the organisation's declarations",
			Do: func() error {
				catalogue, err := distill.LoadCatalogue(orgClonePath)
				if err != nil {
					return err
				}
				distill.Compare(candidates, catalogue, level)
				if !judging || len(candidates) == 0 {
					return nil
				}
				corpus, err = distill.LoadCorpus(orgClonePath, level, teamsOfLevel(services, orgClonePath, level), userSlugOf(organization), candidates)
				if err != nil {
					return err
				}
				corpus.Labels = registryLabels(services, orgClonePath)
				result.Brief = distill.Brief(level, candidates, corpus)
				return nil
			},
		},
	}
	if judging && !request.DryRun {
		steps = append(steps, Step{
			Name: "asking the judge " + services.Judge.Name() + ", which takes a minute or more",
			Do: func() error {
				if len(candidates) == 0 {
					return nil
				}
				reason, available := services.Judge.Available()
				if !available {
					report.Judge = &distill.JudgeRun{Backend: services.Judge.Name(), Status: distill.JudgeSkipped, Problems: []string{reason}}
					return nil
				}
				run := services.Judge.JudgeCandidates(level, candidates, corpus)
				report.Judge = &run
				return nil
			},
		})
	}
	if !request.DryRun {
		steps = append(steps, Step{
			Name: "writing the distillation",
			Do: func() error {
				report.Candidates = distill.Summarise(level, candidates)
				result.Report = report
				var err error
				result.Folder, err = services.Distillations.Write(path.Base(strings.ReplaceAll(level, ".", "/")), candidates, report)
				return err
			},
		})
	}
	err = runSteps(services.Progress, steps)
	if request.DryRun {
		report.Candidates = distill.Summarise(level, candidates)
		result.Report = report
	}
	if err != nil {
		return DistillResult{}, err
	}
	return result, nil
}

// distillSources finds the level and the snapshots a request means.
func distillSources(orchestrator Orchestrator, request DistillRequest) (level string, organization machine.OrganizationRef, stored []snapshots.Stored, err error) {
	services := orchestrator.Services
	if request.SnapshotPath != "" {
		snapshot, err := services.Snapshots.Read(request.SnapshotPath)
		if err != nil {
			return "", machine.OrganizationRef{}, nil, refusal.New(err.Error())
		}
		stored = []snapshots.Stored{snapshot}
		if snapshot.Project != "" {
			organization, err = organizationOfAddress(services, snapshot.Project)
			return snapshot.Project, organization, stored, err
		}
		request.Home = true
	}
	if request.Home {
		organization, _, err := resolveOrganization(services, request.Organization)
		if err != nil {
			return "", machine.OrganizationRef{}, nil, err
		}
		if stored == nil {
			stored, err = services.Snapshots.Latest("")
			if err != nil {
				return "", machine.OrganizationRef{}, nil, err
			}
		}
		return "users." + userSlugOf(organization), organization, stored, nil
	}
	var address string
	if request.Project != "" {
		organization, _, err = resolveOrganization(services, request.Organization)
		if err != nil {
			return "", machine.OrganizationRef{}, nil, err
		}
		address = projectAddress(organization.Slug, request.Project)
	} else {
		location, err := services.WorkingCopy.Locate(request.Directory)
		if err != nil {
			return "", machine.OrganizationRef{}, nil, refusal.New(fmt.Sprintf("%s is not inside a git repository with a commit: %v; name the project with --project", request.Directory, err))
		}
		registered, err := services.Organizations.ProjectClones()
		if err != nil {
			return "", machine.OrganizationRef{}, nil, err
		}
		for _, project := range registered {
			if project.ClonePath == location.Folder {
				address = project.Address
			}
		}
		if address == "" {
			return "", machine.OrganizationRef{}, nil, refusal.New(fmt.Sprintf("%s is not initialised; run rein init first, which snapshots its harness", location.Folder))
		}
		organization, err = organizationOfAddress(services, address)
		if err != nil {
			return "", machine.OrganizationRef{}, nil, err
		}
	}
	stored, err = services.Snapshots.Latest(address)
	if err != nil {
		return "", machine.OrganizationRef{}, nil, err
	}
	return address, organization, stored, nil
}

// organizationOfAddress finds the connected organisation an address starts with.
func organizationOfAddress(services Services, declarationAddress string) (machine.OrganizationRef, error) {
	segments := strings.Split(declarationAddress, ".")
	if len(segments) < 2 || segments[0] != "organizations" {
		return machine.OrganizationRef{}, refusal.New(fmt.Sprintf("%s is not an address inside an organisation", declarationAddress))
	}
	organization, _, err := services.Organizations.Lookup(segments[1])
	return organization, err
}

// teamsOfLevel are the teams a project level belongs to, for the resolved
// set; a team level is its own team; other levels have none.
func teamsOfLevel(services Services, orgClonePath string, level string) []string {
	segments := strings.Split(level, ".")
	if len(segments) != 4 || segments[0] != "organizations" {
		return nil
	}
	switch segments[2] {
	case "teams":
		return []string{segments[3]}
	case "projects":
		tree, err := services.Records.Tree(orgClonePath)
		if err != nil {
			return nil
		}
		return tree.Projects[segments[3]].Teams
	}
	return nil
}

// registryLabels lists the organisation's label registry as category:value,
// for the judge; an unreadable registry gives none.
func registryLabels(services Services, orgClonePath string) []string {
	tree, err := services.Records.Tree(orgClonePath)
	if err != nil {
		return nil
	}
	var labels []string
	for _, category := range sortedKeys(tree.Labels) {
		for _, value := range tree.Labels[category].LabelValues() {
			labels = append(labels, category+":"+value)
		}
	}
	return labels
}
