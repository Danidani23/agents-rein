package orchestration

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Danidani23/agents-rein/internal/app/2_services/address"
	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/distill"
	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
	"github.com/Danidani23/agents-rein/internal/app/2_services/proposal"
	"github.com/Danidani23/agents-rein/internal/app/2_services/proposalfiles"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
	"github.com/Danidani23/agents-rein/internal/app/2_services/resolve"
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
	// Model names the distiller's model, "" for the backend's default.
	Model string
	// DryRun stops before asking the judge and before writing anything,
	// with the brief in the result.
	DryRun bool
	// Force replaces a pending proposal of the same snapshot.
	Force bool
}

// DistillResult is what DistillHarness did: the proposal it wrote and
// where, or the brief on a dry run, and the command that opens the
// distiller's conversation, "" without one. Reaches names, by kind/id,
// the projects each candidate for the organisation or a team reaches.
type DistillResult struct {
	Level        string
	File         string
	Proposal     proposal.Proposal
	Brief        string
	Conversation string
	Reaches      map[string][]string
}

// DistillHarness turns the harness a snapshot holds into candidate
// declarations at its level, compares them with the organisation's
// declarations, asks the judge where each belongs, and writes it all as one
// pending proposal: under .rein/proposals/ of the project's folder, or
// under the home's pending folder for the agents' homes. It never touches
// the org clone.
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
	result := DistillResult{Level: level}
	document := proposal.Proposal{Version: proposal.Version, Status: proposal.StatusPending, Organization: organization.Slug, DistilledFrom: level, Skipped: []string{}, Candidates: []proposal.Candidate{}}
	var candidates []distill.Candidate
	var corpus distill.Corpus
	var catalogue distill.Catalogue
	// the folder the proposal goes into: the project clone's, else the home's
	folder := services.ProposalFiles.HomePendingFolder()
	if stored[0].Project != "" && folderExists(stored[0].Folder) {
		folder = proposalfiles.PendingFolderOf(stored[0].Folder)
	}
	judging := request.Judge != "none"
	steps := []Step{
		{
			Name: "reading the snapshot",
			Do: func() error {
				var contents [][]byte
				for _, snapshot := range stored {
					content, err := os.ReadFile(snapshot.Path)
					if err != nil {
						return err
					}
					contents = append(contents, content)
					if snapshot.Agent != "claude-code" {
						document.Skipped = append(document.Skipped, fmt.Sprintf("%s: distill knows no harness of %s yet", snapshot.Path, snapshot.Agent))
						continue
					}
					converted, skipped, err := distill.ConvertClaudeCode(snapshot.Files)
					if err != nil {
						return err
					}
					candidates = append(candidates, converted...)
					document.Skipped = append(document.Skipped, skipped...)
				}
				digest := proposal.DigestOf(contents...)
				document.ID = digest[:12]
				document.Snapshot = proposal.Snapshot{Path: stored[0].Path, SHA256: digest, TakenAt: stored[0].TakenAt}
				// a pending proposal is refused before the distiller spends minutes on it (Q44)
				if request.DryRun {
					return nil
				}
				err := services.ProposalFiles.RefusePending(folder, document.ID, request.Force)
				if err != nil {
					return refusal.New(err.Error())
				}
				return nil
			},
		},
		{
			Name: "comparing with the organisation's declarations",
			Do: func() error {
				var err error
				catalogue, err = distill.LoadCatalogue(orgClonePath)
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
				corpus.Labels, corpus.SingleValued = registryLabels(services, orgClonePath)
				corpus.ProjectLabels = projectLabelsOf(services, orgClonePath, level)
				corpus.Projects = projectsOf(services, orgClonePath)
				// a document goes beside the clone's tracked Markdown, so only a project clone offers it
				if stored[0].Project != "" && folderExists(stored[0].Folder) {
					documents, err := services.WorkingCopy.TrackedMarkdown(stored[0].Folder)
					if err == nil {
						corpus.Documents = documents
					}
					// what the project is: its README's first part and its files' names (Q41 RD2)
					corpus.ProjectReadme = distill.ReadmeExcerpt(readmeOf(stored[0].Folder))
					tracked, err := services.WorkingCopy.TrackedFiles(stored[0].Folder)
					if err == nil {
						corpus.ProjectFiles = distill.ProjectFilesOf(tracked)
						corpus.ProjectFilesTotal = len(tracked)
					}
				}
				result.Brief = distill.Brief(level, candidates, corpus)
				return nil
			},
		},
	}
	if judging && !request.DryRun {
		steps = append(steps, Step{
			Name: "asking the distiller " + services.Judge.Name() + ", which can take several minutes",
			Do: func() error {
				if len(candidates) == 0 {
					return nil
				}
				// a distiller that is not there is said, never skipped in silence (Q42)
				reason, available := services.Judge.Available()
				if !available {
					return refusal.New(fmt.Sprintf("the distiller %s is not available: %s; install or log in to it, or take rein's own cut alone with --distiller none", services.Judge.Name(), reason))
				}
				document.Judge, candidates = services.Judge.JudgeCandidates(level, candidates, corpus, request.Model)
				// a failed distiller leaves nothing behind, so the next distill starts fresh
				if document.Judge.Status == distill.JudgeFailed {
					return refusal.New(fmt.Sprintf("the distiller %s failed, so nothing was written: %s; distill again, name a faster model with --model, or take rein's own cut alone with --distiller none", document.Judge.Backend, strings.Join(document.Judge.Problems, "; ")))
				}
				// the units the distiller cut out are compared like any candidate
				distill.Compare(candidates, catalogue, level)
				return nil
			},
		})
	}
	if !request.DryRun {
		steps = append(steps, Step{
			Name: "writing the proposal",
			Do: func() error {
				if document.Judge.Backend == "" {
					document.Judge = distill.JudgeRun{Backend: services.Judge.Name(), Status: distill.JudgeSkipped, Problems: []string{"not asked"}}
				}
				document.Judge.Problems = orEmptyList(document.Judge.Problems)
				document.Judge.Dropped = orEmptyList(document.Judge.Dropped)
				document.DistilledAt = services.Clock()
				for _, candidate := range candidates {
					document.Candidates = append(document.Candidates, placeJudged(services, orgClonePath, organization, level, candidate))
				}
				if stored[0].Project != "" && folderExists(stored[0].Folder) {
					_, err := services.WorkingCopy.WriteIgnoreBlock(stored[0].Folder, ignoreEntries(services))
					if err != nil {
						return err
					}
				}
				var err error
				result.File, err = services.ProposalFiles.WritePending(folder, document, request.Force)
				if err != nil {
					return refusal.New(err.Error())
				}
				result.Proposal = document
				result.Conversation = services.Judge.ResumeCommand(document.Judge.Session)
				tree, err := services.Records.Tree(orgClonePath)
				if err == nil {
					result.Reaches = reachesOf(tree, document)
				}
				return nil
			},
		})
	}
	err = runSteps(services.Progress, steps)
	if err != nil {
		return DistillResult{}, err
	}
	if request.DryRun {
		for _, candidate := range candidates {
			document.Candidates = append(document.Candidates, placeJudged(services, orgClonePath, organization, level, candidate))
		}
		result.Proposal = document
	}
	return result, nil
}

// placeJudged turns a distilled candidate into the proposal's form, where
// the judge's view decides the level: a drop leaves it out, a lift moves it
// to the organisation, the project's team or the user's level with its
// labels, and a lift to a team the project does not decide stays with a
// note. Without a judgement the candidate stays at the distilled level.
func placeJudged(services Services, orgClonePath string, organization machine.OrganizationRef, level string, candidate distill.Candidate) proposal.Candidate {
	judgement := candidate.Judgement
	if judgement == nil {
		return proposal.FromCandidate(candidate, level, true, nil, nil)
	}
	// a label goes to the registry, at the organisation, or to the project's record
	if candidate.Kind == distill.KindLabels {
		target := level
		if judgement.Suggestion != distill.LabelAssign {
			target = address.Organizations + "." + organization.Slug
		}
		resolved := &proposal.Judgement{Verdict: judgement.Verdict, Suggestion: judgement.Suggestion, Level: target, ApplyIf: []string{}, Reason: judgement.Reason, Quotes: []distill.Quote{}}
		return proposal.FromCandidate(candidate, target, judgement.Suggestion != distill.LabelRedundant, nil, resolved)
	}
	resolved := &proposal.Judgement{Verdict: judgement.Verdict, Suggestion: judgement.Suggestion, ApplyIf: orEmptyList(judgement.ApplyIf), Everywhere: judgement.Everywhere, Reason: judgement.Reason, Quotes: judgement.Quotes}
	if resolved.Quotes == nil {
		resolved.Quotes = []distill.Quote{}
	}
	target := level
	include := true
	var labels []string
	var note string
	switch judgement.Suggestion {
	case "drop":
		include = false
	case "lift":
		switch judgement.Level {
		case "organization":
			target = address.Organizations + "." + organization.Slug
		case "user":
			target = userLevelOf(organization)
		case "team":
			teams := teamsOfLevel(services, orgClonePath, level)
			switch len(teams) {
			case 1:
				target = address.Organizations + "." + organization.Slug + "." + address.Teams + "." + teams[0]
			case 0:
				note = "the distiller lifts it to the team level; set level to the team's address"
			default:
				note = "the distiller lifts it to the team level and the project belongs to the teams " + strings.Join(teams, ", ") + "; set level to one of them"
			}
		}
		for _, label := range judgement.ApplyIf {
			if _, _, ok := declaration.SplitLabel(label); ok {
				labels = append(labels, label)
			}
		}
	}
	resolved.Level = target
	placed := proposal.FromCandidate(candidate, target, include, labels, resolved)
	if note != "" {
		placed.Notes = append(placed.Notes, note)
	}
	// a contradiction waits for the developer's decision, which propose asks for (D34)
	if judgement.Verdict == "contradicts" {
		placed.Include = false
		placed.Conflict = &proposal.Conflict{With: orEmptyList(judgement.Related), Resolution: ""}
		placed.Notes = append(placed.Notes, "in conflict with "+strings.Join(judgement.Related, ", ")+"; set conflict.resolution in "+proposal.CandidateFileName(placed)+": drop, override or amend")
	}
	return placed
}

// projectLabelsOf is what the project at level carries, an empty list for
// none; nil when level is not a project's or its record is unreadable.
func projectLabelsOf(services Services, orgClonePath string, level string) []string {
	segments := strings.Split(level, ".")
	if len(segments) != 4 || segments[2] != address.Projects {
		return nil
	}
	tree, err := services.Records.Tree(orgClonePath)
	if err != nil {
		return nil
	}
	project, found := tree.Projects[segments[3]]
	if !found {
		return nil
	}
	return append([]string{}, project.Labels...)
}

// projectsOf lists the organisation's projects with their teams and
// labels, for the distiller to see whom a lift reaches; none when the
// records cannot be read.
func projectsOf(services Services, orgClonePath string) []distill.Project {
	tree, err := services.Records.Tree(orgClonePath)
	if err != nil {
		return nil
	}
	var projects []distill.Project
	for _, slug := range sortedKeys(tree.Projects) {
		projects = append(projects, distill.Project{Slug: slug, Teams: tree.Projects[slug].Teams, Labels: tree.Projects[slug].Labels})
	}
	return projects
}

// reachesOf names, by kind/id, the projects each candidate going to the
// organisation or a team reaches, counting the labels the proposal assigns
// to the project it was distilled from (D40).
func reachesOf(tree declaration.Tree, document proposal.Proposal) map[string][]string {
	added := map[string][]string{}
	segments := strings.Split(document.DistilledFrom, ".")
	if len(segments) == 4 && segments[2] == address.Projects {
		for _, candidate := range document.Candidates {
			if candidate.Kind != distill.KindLabels || !candidate.Goes() {
				continue
			}
			for _, content := range candidate.Files {
				change, err := distill.ParseLabelChange(content)
				if err == nil && change.Action == distill.LabelAssign {
					added[segments[3]] = append(added[segments[3]], change.Label)
				}
			}
		}
	}
	reaches := map[string][]string{}
	for _, candidate := range document.Candidates {
		if !candidate.Goes() || candidate.Kind == distill.KindLabels || candidate.Kind == distill.KindDocuments {
			continue
		}
		reached := resolve.Reaches(tree, candidate.Level, declaration.Selectors{ApplyIf: candidate.ApplyIf, DoNotApplyIf: candidate.DoNotApplyIf}, added)
		if reached != nil {
			reaches[candidate.Kind+"/"+candidate.ID] = reached
		}
	}
	return reaches
}

// readmeOf is the README at the top of a project's folder, whatever its
// case and extension, "" when there is none.
func readmeOf(folder string) string {
	entries, err := os.ReadDir(folder)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		name := strings.ToLower(entry.Name())
		if entry.IsDir() || (name != "readme.md" && name != "readme" && name != "readme.txt") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(folder, entry.Name()))
		if err == nil {
			return string(content)
		}
	}
	return ""
}

// folderExists says whether a folder is there to hold a proposal.
func folderExists(folder string) bool {
	info, err := os.Stat(folder)
	return err == nil && info.IsDir()
}

// orEmptyList writes an empty list rather than nothing.
func orEmptyList(items []string) []string {
	if items == nil {
		return []string{}
	}
	return items
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
		return userLevelOf(organization), organization, stored, nil
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
// for the judge, and the categories of one value; an unreadable registry
// gives none.
func registryLabels(services Services, orgClonePath string) (labels []string, singleValued []string) {
	tree, err := services.Records.Tree(orgClonePath)
	if err != nil {
		return nil, nil
	}
	for _, category := range sortedKeys(tree.Labels) {
		for _, value := range tree.Labels[category].LabelValues() {
			labels = append(labels, category+":"+value)
		}
		if !tree.Labels[category].Multiple {
			singleValued = append(singleValued, category)
		}
	}
	return labels, singleValued
}
