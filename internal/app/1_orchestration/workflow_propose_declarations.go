package orchestration

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Danidani23/agents-rein/internal/app/2_services/address"
	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/distill"
	"github.com/Danidani23/agents-rein/internal/app/2_services/lint"
	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
	"github.com/Danidani23/agents-rein/internal/app/2_services/proposal"
	"github.com/Danidani23/agents-rein/internal/app/2_services/proposalfiles"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
	"github.com/Danidani23/agents-rein/internal/app/2_services/scaffold"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// ProposeRequest names what to propose: a proposal file written by distill,
// which says where each candidate goes, or a plain level folder or an
// export zip, which go to Level, the user's own level when empty.
type ProposeRequest struct {
	Source       string
	Level        string
	Organization string
	Title        string
}

// PlannedCandidate is one candidate as the plan shows it.
type PlannedCandidate struct {
	Kind    string
	ID      string
	Level   string
	ApplyIf []string
	// Verdict is the comparison with the organisation: new, identical or
	// same-id, with the related addresses.
	Verdict string
	Related []string
	// Judge is the judge's reason, "" when the candidate was not judged.
	Judge string
	// Reaches are the projects a candidate for the organisation or a team
	// applies to; nil at another level.
	Reaches []string
}

// ProposalPlan is what a proposal would do: each candidate at its level,
// the ones left out as identical to what the organisation has, and the
// ones the file excludes. Making it changes nothing on the host.
type ProposalPlan struct {
	Organization string
	File         string
	ID           string
	Included     []PlannedCandidate
	LeftOut      []PlannedCandidate
	Excluded     []PlannedCandidate
	// Documents are the paths written into the project's clone, for its
	// repository, not the organisation's (D34).
	Documents []string
	// Labels are the changes to the registry and to project records.
	Labels []string
}

// ProposeResult is what ProposeDeclarations did.
type ProposeResult struct {
	ProposalPlan
	Branch         string
	PullRequestURL string
	// HistoryFile is where the proposed file went, "" for a folder or zip.
	HistoryFile string
}

// preparedProposal is the plan with what the workflow needs to carry it
// out.
type preparedProposal struct {
	ProposalPlan
	document     proposal.Proposal
	organization machine.OrganizationRef
	orgClonePath string
	levelID      string
	filesByLevel map[string]map[string]string
	// documents are the documents' contents by path, for cloneFolder, and
	// documentFiles the candidate's file in the proposal that holds each.
	documents     map[string]string
	documentFiles map[string]string
	cloneFolder   string
	// recordFiles are the registry and project record files the label
	// changes rewrite, by path under declarations/.
	recordFiles map[string]string
}

// ProposeDeclarations lints the candidates, compares them with the
// organisation, leaves out the ones it already has, writes the rest on a
// branch of the org clone at the level each goes to, pushes it and opens
// the pull request, with the comparison as its body. A proposal file is
// then marked proposed and moved to the history. The org clone ends on
// main either way.
func (orchestrator Orchestrator) ProposeDeclarations(request ProposeRequest) (ProposeResult, error) {
	services := orchestrator.Services
	prepared, err := prepareProposal(services, request)
	if err != nil {
		return ProposeResult{}, err
	}
	accessToken, err := services.Credentials.AccessToken(prepared.organization.Login)
	if err != nil {
		return ProposeResult{}, err
	}
	access := accessOf(prepared.organization)
	result := ProposeResult{ProposalPlan: prepared.ProposalPlan}
	result.Branch = fmt.Sprintf("rein/propose-%s-%s", prepared.levelID, services.Clock().UTC().Format("20060102-150405"))
	title := request.Title
	if title == "" {
		title = proposalTitle(prepared)
	}
	body := pullRequestBody(request.Source, prepared)
	pushed := false
	err = runSteps(services.Progress, []Step{
		{
			Name: "branching the org repository",
			Do:   func() error { return services.Proposals.Branch(prepared.orgClonePath, result.Branch) },
			Undo: func() error { return services.Proposals.Abandon(access, prepared.orgClonePath, result.Branch, pushed) },
		},
		{
			Name: "writing and pushing the proposal",
			Do: func() error {
				var relativePaths []string
				for _, level := range sortedKeys(prepared.filesByLevel) {
					written, err := services.Proposals.Write(prepared.orgClonePath, levelPathOf(level), prepared.filesByLevel[level])
					if err != nil {
						return err
					}
					relativePaths = append(relativePaths, written...)
				}
				if len(prepared.recordFiles) > 0 {
					written, err := services.Proposals.Write(prepared.orgClonePath, "", prepared.recordFiles)
					if err != nil {
						return err
					}
					relativePaths = append(relativePaths, written...)
				}
				err := services.Proposals.Publish(access, prepared.orgClonePath, result.Branch, relativePaths, title+" (rein propose)")
				if err != nil {
					return err
				}
				pushed = true
				return nil
			},
		},
		{
			Name: "opening the pull request",
			Do: func() error {
				var err error
				result.PullRequestURL, err = services.Proposals.OpenPullRequest(accessToken, prepared.organization.Owner, prepared.organization.Repository, ports.PullRequest{Branch: result.Branch, Title: title, Body: body})
				return err
			},
		},
	})
	if err != nil {
		return ProposeResult{}, err
	}
	err = services.Proposals.BackToMain(prepared.orgClonePath, result.Branch)
	if err != nil {
		return ProposeResult{}, err
	}
	if len(prepared.documents) > 0 {
		_, err = services.WorkingCopy.WriteFiles(prepared.cloneFolder, prepared.documents)
		if err != nil {
			return result, fmt.Errorf("the pull request %s is open, but the documents could not be written into %s: %w", result.PullRequestURL, prepared.cloneFolder, err)
		}
	}
	if prepared.File != "" {
		document := prepared.document
		document.MarkProposed(result.Branch, result.PullRequestURL, services.Clock())
		result.HistoryFile, err = services.ProposalFiles.MoveToHistory(prepared.File, document)
		if err != nil {
			return result, fmt.Errorf("the pull request %s is open, but the proposal could not be moved to the history: %w", result.PullRequestURL, err)
		}
	}
	return result, nil
}

// prepareProposal reads the source, checks it, compares every included
// candidate with the organisation at its level and lints the result. It
// pulls the org clone and writes nothing.
func prepareProposal(services Services, request ProposeRequest) (preparedProposal, error) {
	document, isFile, err := loadProposal(services, request)
	if err != nil {
		return preparedProposal{}, err
	}
	if len(document.Candidates) == 0 {
		return preparedProposal{}, refusal.New(fmt.Sprintf("%s holds no declarations", request.Source))
	}
	organization, orgClonePath, err := services.Organizations.Lookup(document.Organization)
	if err != nil {
		return preparedProposal{}, err
	}
	if organization.Owner == "" || organization.Repository == "" {
		return preparedProposal{}, refusal.New(fmt.Sprintf("the org repository of %s is not on GitHub (%s); a pull request needs one", organization.Slug, organization.Remote))
	}
	err = services.Records.Pull(accessOf(organization), orgClonePath)
	if err != nil {
		return preparedProposal{}, err
	}
	tree, err := services.Records.Tree(orgClonePath)
	if err != nil {
		return preparedProposal{}, err
	}
	reaches := reachesOf(tree, document)
	registry, recordFiles, labelChanges, err := applyLabelChanges(document, tree, services.Clock())
	if err != nil {
		return preparedProposal{}, err
	}
	problems := document.Check(registry)
	if len(problems) > 0 {
		return preparedProposal{}, refusal.New("the proposal cannot go as it is:\n  " + strings.Join(problems, "\n  "))
	}
	catalogue, err := distill.LoadCatalogue(orgClonePath)
	if err != nil {
		return preparedProposal{}, err
	}
	// a required declaration may not be weakened by anything below it
	enforcements := map[string]string{}
	for _, entry := range catalogue.Entries {
		enforcements[entry.Address] = entry.Enforcement
	}
	var overrides []string
	for _, candidate := range document.Candidates {
		if candidate.Conflict == nil || candidate.Conflict.Resolution != proposal.ResolutionOverride {
			continue
		}
		for _, with := range candidate.Conflict.With {
			if enforcements[with] == declaration.EnforcementRequired {
				overrides = append(overrides, fmt.Sprintf("%s/%s cannot override %s, which is required; drop it, or amend the required declaration", candidate.Kind, candidate.ID, with))
			}
		}
	}
	if len(overrides) > 0 {
		return preparedProposal{}, refusal.New("the proposal cannot go as it is:\n  " + strings.Join(overrides, "\n  "))
	}
	prepared := preparedProposal{
		ProposalPlan:  ProposalPlan{Organization: organization.Slug, ID: document.ID},
		document:      document,
		organization:  organization,
		orgClonePath:  orgClonePath,
		levelID:       levelIDOf(document.DistilledFrom),
		filesByLevel:  map[string]map[string]string{},
		documents:     map[string]string{},
		documentFiles: map[string]string{},
		recordFiles:   recordFiles,
	}
	prepared.Labels = labelChanges
	if isFile {
		prepared.File = request.Source
	}
	levels, byLevel := document.Included()
	for _, level := range levels {
		var declared []proposal.Candidate
		for _, candidate := range byLevel[level] {
			if candidate.Kind == distill.KindLabels {
				continue
			}
			if candidate.Kind == distill.KindDocuments {
				// a document goes into the project's clone, not the org repository
				documentPath := path.Clean(candidate.Path)
				for _, content := range candidate.Files {
					prepared.documents[documentPath] = content
				}
				prepared.documentFiles[documentPath] = proposal.CandidateFileName(candidate)
				prepared.Documents = append(prepared.Documents, documentPath)
				continue
			}
			declared = append(declared, candidate)
		}
		var group []distill.Candidate
		for _, candidate := range declared {
			files, err := proposal.FilesOf(candidate)
			if err != nil {
				return preparedProposal{}, fmt.Errorf("%s/%s: %w", candidate.Kind, candidate.ID, err)
			}
			group = append(group, distill.Candidate{Kind: candidate.Kind, ID: candidate.ID, Files: files})
		}
		distill.Compare(group, catalogue, level)
		for index, compared := range group {
			planned := plannedOf(declared[index], compared)
			planned.Reaches = reaches[planned.Kind+"/"+planned.ID]
			if compared.Verdict == distill.VerdictIdentical {
				prepared.LeftOut = append(prepared.LeftOut, planned)
				continue
			}
			prepared.Included = append(prepared.Included, planned)
			if prepared.filesByLevel[level] == nil {
				prepared.filesByLevel[level] = map[string]string{}
			}
			for relative, content := range compared.Files {
				prepared.filesByLevel[level][relative] = content
			}
		}
	}
	for _, candidate := range document.Excluded() {
		prepared.Excluded = append(prepared.Excluded, plannedOf(candidate, distill.Candidate{}))
	}
	if len(prepared.Documents) > 0 {
		prepared.cloneFolder = cloneOfProposal(request.Source)
		if prepared.cloneFolder == "" {
			return preparedProposal{}, refusal.New("a document goes into the project's clone; propose the proposal the clone holds under .rein/proposals/")
		}
		for _, documentPath := range prepared.Documents {
			_, err := os.Stat(filepath.Join(prepared.cloneFolder, filepath.FromSlash(documentPath)))
			if err == nil {
				return preparedProposal{}, refusal.New(fmt.Sprintf("%s exists in the clone; change path in %s, or merge the text by hand", documentPath, prepared.documentFiles[documentPath]))
			}
		}
	}
	if len(prepared.Included) == 0 && len(prepared.recordFiles) == 0 {
		return preparedProposal{}, refusal.New("nothing to propose: every included candidate is identical to a declaration the organisation has")
	}
	byPath := map[string]map[string]string{}
	for level, files := range prepared.filesByLevel {
		byPath[levelPathOf(level)] = files
	}
	err = lintCandidates(byPath)
	if err != nil {
		return preparedProposal{}, err
	}
	for _, level := range sortedKeys(prepared.filesByLevel) {
		levelPath := levelPathOf(level)
		if !strings.HasPrefix(levelPath, address.Users+"/") {
			continue
		}
		userRecord, err := userRecordIfMissing(orgClonePath, levelPath, path.Base(levelPath), organization, services)
		if err != nil {
			return preparedProposal{}, err
		}
		for relative, content := range userRecord {
			prepared.filesByLevel[level][relative] = content
		}
	}
	return prepared, nil
}

// loadProposal reads the source as a proposal when it is one, a folder
// holding proposal.yaml or a file from before folders, and otherwise turns
// a level folder or an export zip into a proposal of its files at the
// requested level, the user's own by default.
func loadProposal(services Services, request ProposeRequest) (document proposal.Proposal, isFile bool, err error) {
	info, err := os.Stat(request.Source)
	if err != nil {
		return proposal.Proposal{}, false, refusal.New(fmt.Sprintf("cannot read %s: %v", request.Source, err))
	}
	_, indexErr := os.Stat(filepath.Join(request.Source, proposal.IndexFile))
	isProposal := (info.IsDir() && indexErr == nil) || (!info.IsDir() && strings.HasSuffix(request.Source, ".yaml"))
	if isProposal {
		document, err := services.ProposalFiles.Read(request.Source)
		if err != nil {
			return proposal.Proposal{}, false, refusal.New(err.Error())
		}
		if request.Level != "" {
			return proposal.Proposal{}, false, refusal.New("a proposal says where each candidate goes; edit level in each candidate's file instead of --level")
		}
		return document, true, nil
	}
	candidates, err := services.Proposals.Read(request.Source)
	if err != nil {
		return proposal.Proposal{}, false, refusal.New(fmt.Sprintf("cannot read %s: %v", request.Source, err))
	}
	level := request.Level
	var organization machine.OrganizationRef
	if level == "" {
		organization, _, err = resolveOrganization(services, request.Organization)
		if err != nil {
			return proposal.Proposal{}, false, err
		}
		level = userLevelOf(organization)
	} else {
		parsed, err := address.Parse(level)
		if err != nil || parsed.IsDeclaration() {
			return proposal.Proposal{}, false, refusal.New(fmt.Sprintf("%q is not a level: organizations.<org>, organizations.<org>.teams.<team>, organizations.<org>.projects.<project> or users.<user>", level))
		}
		organization, _, err = organizationOfLevel(services, level, request.Organization)
		if err != nil {
			return proposal.Proposal{}, false, err
		}
	}
	document = proposal.Proposal{Version: proposal.Version, Status: proposal.StatusPending, Organization: organization.Slug, DistilledFrom: level, Skipped: []string{}}
	for _, candidate := range groupCandidates(candidates.Files) {
		document.Candidates = append(document.Candidates, proposal.FromCandidate(candidate, level, true, nil, nil))
	}
	return document, false, nil
}

// applyLabelChanges applies the label candidates that go to a copy of the
// registry and of the project records: additions first, so an assignment
// or an applyIf may use a label added in the same proposal. It returns the
// registry the candidates are checked against, the record files to write
// under declarations/, and what changed.
func applyLabelChanges(document proposal.Proposal, tree declaration.Tree, now time.Time) (map[string]declaration.LabelCategory, map[string]string, []string, error) {
	registry := map[string]declaration.LabelCategory{}
	for name, category := range tree.Labels {
		copied := category
		copied.Labels = map[string]string{}
		for value, meaning := range category.Labels {
			copied.Labels[value] = meaning
		}
		registry[name] = copied
	}
	files := map[string]string{}
	var changes []string
	projects := map[string]declaration.Project{}
	for _, action := range []string{distill.LabelAdd, distill.LabelAssign} {
		for _, candidate := range document.Candidates {
			if candidate.Kind != distill.KindLabels || !candidate.Goes() {
				continue
			}
			var change distill.LabelSuggestion
			for _, content := range candidate.Files {
				parsed, err := distill.ParseLabelChange(content)
				if err != nil {
					return nil, nil, nil, refusal.New(fmt.Sprintf("%s/%s: %v", candidate.Kind, candidate.ID, err))
				}
				change = parsed
			}
			if change.Action != action {
				continue
			}
			where := candidate.Kind + "/" + candidate.ID
			categoryName, value, ok := declaration.SplitLabel(change.Label)
			if !ok {
				return nil, nil, nil, refusal.New(fmt.Sprintf("%s: %q is not category:value", where, change.Label))
			}
			switch action {
			case distill.LabelAdd:
				category, exists := registry[categoryName]
				if !exists {
					category = declaration.LabelCategory{Category: categoryName, Multiple: change.Multiple != nil && *change.Multiple, Labels: map[string]string{}}
				}
				if _, taken := category.Labels[value]; taken {
					return nil, nil, nil, refusal.New(fmt.Sprintf("%s: %s is in the registry already; assign it instead", where, change.Label))
				}
				category.Labels[value] = change.Meaning
				registry[categoryName] = category
				relativePath, content, err := scaffold.LabelCategoryRecord(category)
				if err != nil {
					return nil, nil, nil, err
				}
				files[strings.TrimPrefix(relativePath, "declarations/")] = content
				changes = append(changes, fmt.Sprintf("add %s: %s", change.Label, change.Meaning))
			case distill.LabelAssign:
				slug := path.Base(strings.ReplaceAll(candidate.Level, ".", "/"))
				project, found := projects[slug]
				if !found {
					project, found = tree.Projects[slug]
					project.Labels = append([]string{}, project.Labels...)
				}
				if !found {
					return nil, nil, nil, refusal.New(fmt.Sprintf("%s: %s is not a project's level", where, candidate.Level))
				}
				if _, known := registry[categoryName].Labels[value]; !known {
					return nil, nil, nil, refusal.New(fmt.Sprintf("%s: %s is not in the registry; add it instead", where, change.Label))
				}
				if !contains(project.Labels, change.Label) {
					project.Labels = append(project.Labels, change.Label)
				}
				conflicts := declaration.SingleValueConflicts(registry, project.Labels)
				if len(conflicts) > 0 {
					return nil, nil, nil, refusal.New(fmt.Sprintf("%s: the project %s would carry %s", where, slug, strings.Join(conflicts, "; ")))
				}
				project.UpdatedAt = now
				projects[slug] = project
				relativePath, content, err := scaffold.ProjectRecord(project)
				if err != nil {
					return nil, nil, nil, err
				}
				files[strings.TrimPrefix(relativePath, "declarations/")] = content
				changes = append(changes, fmt.Sprintf("assign %s to %s", change.Label, candidate.Level))
			}
		}
	}
	return registry, files, changes, nil
}

// cloneOfProposal is the project clone a pending proposal lives in, under
// .rein/proposals/; "" for any other source.
func cloneOfProposal(source string) string {
	proposals := filepath.Dir(source)
	reinFolder := filepath.Dir(proposals)
	if filepath.Base(proposals) != "proposals" || filepath.Base(reinFolder) != proposalfiles.CloneFolder {
		return ""
	}
	return filepath.Dir(reinFolder)
}

// plannedOf is a candidate as the plan shows it, with the comparison when
// one was made.
func plannedOf(candidate proposal.Candidate, compared distill.Candidate) PlannedCandidate {
	planned := PlannedCandidate{Kind: candidate.Kind, ID: candidate.ID, Level: candidate.Level, ApplyIf: candidate.ApplyIf, Verdict: compared.Verdict, Related: compared.Related}
	if candidate.Judge != nil {
		planned.Judge = candidate.Judge.Suggestion + ": " + candidate.Judge.Reason
		if candidate.Judge.Everywhere != "" {
			planned.Judge += "; every project: " + candidate.Judge.Everywhere
		}
	}
	if candidate.Conflict != nil && candidate.Judge != nil {
		with := strings.Join(candidate.Conflict.With, ", ")
		planned.Judge = "conflict with " + with + ", resolved " + candidate.Conflict.Resolution + ": " + candidate.Judge.Reason
		if candidate.Conflict.Resolution == proposal.ResolutionAmend {
			planned.Judge += "; amend " + with + " in a proposal of its own"
		}
	}
	return planned
}

// levelIDOf is the last slug of a level, for the branch name.
func levelIDOf(level string) string {
	parsed, err := address.Parse(level)
	if err != nil {
		return "level"
	}
	return parsed.ID()
}

// proposalTitle names the pull request: the one level, or the count of
// levels when the candidates go to several.
func proposalTitle(prepared preparedProposal) string {
	levels := sortedKeys(prepared.filesByLevel)
	if len(levels) == 1 {
		return fmt.Sprintf("Propose %d declarations to %s", len(prepared.Included), levels[0])
	}
	return fmt.Sprintf("Propose %d declarations to %d levels of %s", len(prepared.Included), len(levels), prepared.Organization)
}

// organizationOfLevel is the organisation a level belongs to, the first
// pair of its address; a different organisation named is a refusal.
func organizationOfLevel(services Services, level string, named string) (machine.OrganizationRef, string, error) {
	parsed, err := address.Parse(level)
	if err != nil {
		return machine.OrganizationRef{}, "", refusal.New(err.Error())
	}
	slug := parsed.OrganizationSlug()
	if named != "" && named != slug {
		return machine.OrganizationRef{}, "", refusal.New(fmt.Sprintf("the level %s belongs to %s, not %s", level, slug, named))
	}
	return services.Organizations.Lookup(slug)
}

// groupCandidates turns files into one candidate per declaration, so the
// comparison sees skills and hooks whole.
func groupCandidates(files map[string]string) []distill.Candidate {
	byKey := map[string]*distill.Candidate{}
	var keys []string
	for relative, content := range files {
		segments := strings.Split(relative, "/")
		if len(segments) < 2 {
			continue
		}
		kind := segments[0]
		id := strings.TrimSuffix(strings.TrimSuffix(segments[1], ".md"), ".yaml")
		key := kind + "/" + id
		candidate, found := byKey[key]
		if !found {
			candidate = &distill.Candidate{Kind: kind, ID: id, Files: map[string]string{}}
			byKey[key] = candidate
			keys = append(keys, key)
		}
		candidate.Files[relative] = content
		candidate.Sources = append(candidate.Sources, relative)
	}
	sort.Strings(keys)
	var grouped []distill.Candidate
	for _, key := range keys {
		sort.Strings(byKey[key].Sources)
		grouped = append(grouped, *byKey[key])
	}
	return grouped
}

// lintCandidates writes the files under a scratch declarations tree, each
// level at its path, and refuses on any error the lint finds.
func lintCandidates(filesByLevel map[string]map[string]string) error {
	scratch, err := os.MkdirTemp("", "rein-propose-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)
	for levelPath, files := range filesByLevel {
		for relative, content := range files {
			target := filepath.Join(scratch, "declarations", filepath.FromSlash(levelPath), filepath.FromSlash(relative))
			err = os.MkdirAll(filepath.Dir(target), 0o755)
			if err != nil {
				return err
			}
			err = os.WriteFile(target, []byte(content), 0o644)
			if err != nil {
				return err
			}
		}
	}
	report, err := lint.Run(scratch)
	if err != nil {
		return err
	}
	if report.Errors() == 0 {
		return nil
	}
	var messages []string
	for _, finding := range report.Findings {
		if finding.Severity == "error" {
			messages = append(messages, strings.TrimPrefix(finding.Path, scratch+string(filepath.Separator))+": "+finding.Message)
		}
	}
	if len(messages) > 10 {
		messages = append(messages[:10], fmt.Sprintf("and %d more", len(messages)-10))
	}
	return refusal.New("the candidates do not pass the lint:\n  " + strings.Join(messages, "\n  "))
}

// userRecordIfMissing adds the user's record to the proposal when the org
// repository has none yet, so the level exists.
func userRecordIfMissing(orgClonePath string, levelPath string, userSlug string, organization machine.OrganizationRef, services Services) (map[string]string, error) {
	_, err := os.Stat(filepath.Join(orgClonePath, "declarations", filepath.FromSlash(levelPath), "user.yaml"))
	if err == nil {
		return nil, nil
	}
	who := ports.Identity{Login: organization.Login, Slug: userSlugOf(organization)}
	if who.Slug != userSlug {
		return nil, refusal.New(fmt.Sprintf("the level %s is %s's; a user's level is proposed by that user", levelPath, userSlug))
	}
	_, content, err := scaffold.UserRecord(who, services.Clock())
	if err != nil {
		return nil, err
	}
	return map[string]string{"user.yaml": content}, nil
}

// pullRequestBody is the comparison as the reviewer reads it: each
// candidate with its level and labels, the judge's view, what the file
// excluded, and what was left out.
func pullRequestBody(source string, prepared preparedProposal) string {
	var body strings.Builder
	fmt.Fprintf(&body, "Proposed by `rein propose` from `%s`, distilled from `%s`.\n\n", filepath.Base(source), prepared.document.DistilledFrom)
	fmt.Fprintf(&body, "| kind | id | level | labels | compared with the organisation | the distiller |\n|---|---|---|---|---|---|\n")
	for _, planned := range prepared.Included {
		fmt.Fprintf(&body, "| %s | %s | %s | %s | %s | %s |\n", planned.Kind, planned.ID, planned.Level, strings.Join(planned.ApplyIf, " "), describeVerdict(planned), escapeCell(orDash(planned.Judge)))
	}
	var reached []string
	for _, planned := range prepared.Included {
		if planned.Reaches == nil {
			continue
		}
		projects := "no project yet"
		if len(planned.Reaches) > 0 {
			projects = strings.Join(planned.Reaches, ", ")
		}
		reached = append(reached, fmt.Sprintf("- %s/%s at %s: %s", planned.Kind, planned.ID, planned.Level, projects))
	}
	if len(reached) > 0 {
		fmt.Fprintf(&body, "\nThe projects each declaration for the organisation or a team reaches:\n%s\n", strings.Join(reached, "\n"))
	}
	if prepared.document.Judge.Backend != "" {
		fmt.Fprintf(&body, "\nDistiller: %s, %s", prepared.document.Judge.Backend, prepared.document.Judge.Status)
		if prepared.document.Judge.Session != "" {
			fmt.Fprintf(&body, ", session %s", prepared.document.Judge.Session)
		}
		if len(prepared.document.Judge.Problems) > 0 {
			fmt.Fprintf(&body, ": %s", strings.Join(prepared.document.Judge.Problems, "; "))
		}
		fmt.Fprintf(&body, "\n")
	}
	if len(prepared.Excluded) > 0 {
		fmt.Fprintf(&body, "\nLeft out by the proposal, with the distiller's view:\n")
		for _, planned := range prepared.Excluded {
			fmt.Fprintf(&body, "- %s/%s: %s\n", planned.Kind, planned.ID, orDash(planned.Judge))
		}
	}
	if len(prepared.LeftOut) > 0 {
		fmt.Fprintf(&body, "\nLeft out, identical to what the organisation has:\n")
		for _, planned := range prepared.LeftOut {
			fmt.Fprintf(&body, "- %s/%s = %s\n", planned.Kind, planned.ID, strings.Join(planned.Related, ", "))
		}
	}
	var notes []string
	for _, candidate := range prepared.document.Candidates {
		for _, note := range candidate.Notes {
			notes = append(notes, fmt.Sprintf("%s/%s: %s", candidate.Kind, candidate.ID, note))
		}
	}
	if len(notes) > 0 {
		fmt.Fprintf(&body, "\nNotes from the distillation:\n")
		for _, note := range notes {
			fmt.Fprintf(&body, "- %s\n", note)
		}
	}
	if len(prepared.Documents) > 0 {
		fmt.Fprintf(&body, "\nDocuments for the project's repository, written into its clone and left for the developer to commit:\n")
		for _, documentPath := range prepared.Documents {
			fmt.Fprintf(&body, "- %s\n", documentPath)
		}
	}
	if len(prepared.Labels) > 0 {
		fmt.Fprintf(&body, "\nLabel changes:\n")
		for _, change := range prepared.Labels {
			fmt.Fprintf(&body, "- %s\n", change)
		}
	}
	if len(prepared.document.Skipped) > 0 {
		fmt.Fprintf(&body, "\nNot distilled:\n")
		for _, skipped := range prepared.document.Skipped {
			fmt.Fprintf(&body, "- %s\n", skipped)
		}
	}
	fmt.Fprintf(&body, "\n`rein lint` passed on these files before the proposal; `rein review` judges the prose against the levels it meets.\n")
	return body.String()
}

func describeVerdict(planned PlannedCandidate) string {
	switch planned.Verdict {
	case distill.VerdictSameID:
		return "the same id exists at " + strings.Join(planned.Related, ", ") + "; resolution replaces or refuses it by enforcement"
	default:
		return "new"
	}
}

func escapeCell(text string) string {
	return strings.ReplaceAll(text, "|", "\\|")
}

func orDash(text string) string {
	if text == "" {
		return "not judged"
	}
	return text
}
