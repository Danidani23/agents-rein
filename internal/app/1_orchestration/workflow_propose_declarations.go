package orchestration

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Danidani23/agents-rein/internal/app/2_services/address"
	"github.com/Danidani23/agents-rein/internal/app/2_services/distill"
	"github.com/Danidani23/agents-rein/internal/app/2_services/lint"
	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
	"github.com/Danidani23/agents-rein/internal/app/2_services/proposal"
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
	problems := document.Check(tree.Labels)
	if len(problems) > 0 {
		return preparedProposal{}, refusal.New("the proposal cannot go as it is:\n  " + strings.Join(problems, "\n  "))
	}
	catalogue, err := distill.LoadCatalogue(orgClonePath)
	if err != nil {
		return preparedProposal{}, err
	}
	prepared := preparedProposal{
		ProposalPlan: ProposalPlan{Organization: organization.Slug, ID: document.ID},
		document:     document,
		organization: organization,
		orgClonePath: orgClonePath,
		levelID:      levelIDOf(document.DistilledFrom),
		filesByLevel: map[string]map[string]string{},
	}
	if isFile {
		prepared.File = request.Source
	}
	levels, byLevel := document.Included()
	for _, level := range levels {
		var group []distill.Candidate
		for _, candidate := range byLevel[level] {
			files, err := proposal.FilesOf(candidate)
			if err != nil {
				return preparedProposal{}, fmt.Errorf("%s/%s: %w", candidate.Kind, candidate.ID, err)
			}
			group = append(group, distill.Candidate{Kind: candidate.Kind, ID: candidate.ID, Files: files})
		}
		distill.Compare(group, catalogue, level)
		for index, compared := range group {
			planned := plannedOf(byLevel[level][index], compared)
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
	if len(prepared.Included) == 0 {
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
			return proposal.Proposal{}, false, refusal.New("a proposal says where each candidate goes; edit its level fields in proposal.yaml instead of --level")
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

// plannedOf is a candidate as the plan shows it, with the comparison when
// one was made.
func plannedOf(candidate proposal.Candidate, compared distill.Candidate) PlannedCandidate {
	planned := PlannedCandidate{Kind: candidate.Kind, ID: candidate.ID, Level: candidate.Level, ApplyIf: candidate.ApplyIf, Verdict: compared.Verdict, Related: compared.Related}
	if candidate.Judge != nil {
		planned.Judge = candidate.Judge.Suggestion + ": " + candidate.Judge.Reason
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
	fmt.Fprintf(&body, "| kind | id | level | labels | compared with the organisation | the judge |\n|---|---|---|---|---|---|\n")
	for _, planned := range prepared.Included {
		fmt.Fprintf(&body, "| %s | %s | %s | %s | %s | %s |\n", planned.Kind, planned.ID, planned.Level, strings.Join(planned.ApplyIf, " "), describeVerdict(planned), escapeCell(orDash(planned.Judge)))
	}
	if prepared.document.Judge.Backend != "" {
		fmt.Fprintf(&body, "\nJudge: %s, %s", prepared.document.Judge.Backend, prepared.document.Judge.Status)
		if prepared.document.Judge.Session != "" {
			fmt.Fprintf(&body, ", session %s", prepared.document.Judge.Session)
		}
		if len(prepared.document.Judge.Problems) > 0 {
			fmt.Fprintf(&body, ": %s", strings.Join(prepared.document.Judge.Problems, "; "))
		}
		fmt.Fprintf(&body, "\n")
	}
	if len(prepared.Excluded) > 0 {
		fmt.Fprintf(&body, "\nLeft out by the proposal, with the judge's view:\n")
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
