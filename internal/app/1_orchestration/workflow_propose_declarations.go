package orchestration

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Danidani23/agents-rein/internal/app/2_services/address"
	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/distill"
	"github.com/Danidani23/agents-rein/internal/app/2_services/lint"
	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
	"github.com/Danidani23/agents-rein/internal/app/2_services/scaffold"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// ProposeRequest names what to propose: a distillation folder, a level
// folder or an export zip, and the level it goes to, which a distillation
// carries in its report and which otherwise defaults to the user's own.
type ProposeRequest struct {
	Source       string
	Level        string
	Organization string
	Title        string
	// AsDistilled keeps every candidate at the distilled level; the judge's
	// placement stays advice in the pull request.
	AsDistilled bool
	// Team is the team a lift to the team level goes to, needed when the
	// project belongs to several.
	Team string
}

// Placement is a candidate the judge moved to another level or dropped,
// with the labels it applies to and the judge's reason.
type Placement struct {
	Kind    string
	ID      string
	From    string
	To      string
	ApplyIf []string
	Reason  string
}

// ProposalPlan is what a proposal would do: each candidate at its level,
// the ones the judge lifted or dropped, and the ones left out as identical
// to what the organisation has. Making it changes nothing on the host.
type ProposalPlan struct {
	Organization string
	Level        string
	Included     []distill.CandidateSummary
	LeftOut      []distill.CandidateSummary
	Lifted       []Placement
	Dropped      []Placement
}

// ProposeResult is what ProposeDeclarations did.
type ProposeResult struct {
	ProposalPlan
	Branch         string
	PullRequestURL string
}

// proposal is the plan with what the workflow needs to carry it out.
type proposal struct {
	ProposalPlan
	organization machine.OrganizationRef
	orgClonePath string
	levelID      string
	report       *distill.Report
	// filesByLevel holds the files to write, by level path.
	filesByLevel map[string]map[string]string
}

// ProposeDeclarations lints the candidates, compares them with the
// organisation, leaves out the ones it already has, places each where the
// judge said, writes the rest on a branch of the org clone, pushes it and
// opens the pull request, with the comparison as its body. The org clone
// ends on main either way.
func (orchestrator Orchestrator) ProposeDeclarations(request ProposeRequest) (ProposeResult, error) {
	services := orchestrator.Services
	plan, err := planProposal(services, request)
	if err != nil {
		return ProposeResult{}, err
	}
	accessToken, err := services.Credentials.AccessToken(plan.organization.Login)
	if err != nil {
		return ProposeResult{}, err
	}
	access := accessOf(plan.organization)
	result := ProposeResult{ProposalPlan: plan.ProposalPlan}
	result.Branch = fmt.Sprintf("rein/propose-%s-%s", plan.levelID, services.Clock().UTC().Format("20060102-150405"))
	title := request.Title
	if title == "" {
		title = fmt.Sprintf("Propose %d declarations to %s", len(plan.Included), plan.Level)
		if len(plan.Lifted) > 0 {
			title += fmt.Sprintf(", %d lifted", len(plan.Lifted))
		}
	}
	body := pullRequestBody(request.Source, plan, request.AsDistilled)
	pushed := false
	err = runSteps(services.Progress, []Step{
		{
			Name: "branching the org repository",
			Do:   func() error { return services.Proposals.Branch(plan.orgClonePath, result.Branch) },
			Undo: func() error { return services.Proposals.Abandon(access, plan.orgClonePath, result.Branch, pushed) },
		},
		{
			Name: "writing and pushing the proposal",
			Do: func() error {
				var relativePaths []string
				for _, levelPath := range sortedKeys(plan.filesByLevel) {
					written, err := services.Proposals.Write(plan.orgClonePath, levelPath, plan.filesByLevel[levelPath])
					if err != nil {
						return err
					}
					relativePaths = append(relativePaths, written...)
				}
				err := services.Proposals.Publish(access, plan.orgClonePath, result.Branch, relativePaths, title+" (rein propose)")
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
				result.PullRequestURL, err = services.Proposals.OpenPullRequest(accessToken, plan.organization.Owner, plan.organization.Repository, ports.PullRequest{Branch: result.Branch, Title: title, Body: body})
				return err
			},
		},
	})
	if err != nil {
		return ProposeResult{}, err
	}
	err = services.Proposals.BackToMain(plan.orgClonePath, result.Branch)
	if err != nil {
		return ProposeResult{}, err
	}
	return result, nil
}

// planProposal reads the source, places every candidate, compares each
// with the organisation at its level and lints the result. It pulls the
// org clone and writes nothing.
func planProposal(services Services, request ProposeRequest) (proposal, error) {
	candidates, err := services.Proposals.Read(request.Source)
	if err != nil {
		return proposal{}, refusal.New(fmt.Sprintf("cannot read %s: %v", request.Source, err))
	}
	if len(candidates.Files) == 0 {
		return proposal{}, refusal.New(fmt.Sprintf("%s holds no declarations", request.Source))
	}
	level := request.Level
	if level == "" && candidates.Report != nil {
		level = candidates.Report.Level
	}
	if level == "" {
		organization, _, err := resolveOrganization(services, request.Organization)
		if err != nil {
			return proposal{}, err
		}
		level = address.Users + "." + userSlugOf(organization)
	}
	parsedLevel, err := address.Parse(level)
	if err != nil || parsedLevel.IsDeclaration() {
		return proposal{}, refusal.New(fmt.Sprintf("%q is not a level: organizations.<org>, organizations.<org>.teams.<team>, organizations.<org>.projects.<project> or users.<user>", level))
	}
	organization, orgClonePath, err := organizationOfLevel(services, level, request.Organization)
	if err != nil {
		return proposal{}, err
	}
	if organization.Owner == "" || organization.Repository == "" {
		return proposal{}, refusal.New(fmt.Sprintf("the org repository of %s is not on GitHub (%s); a pull request needs one", organization.Slug, organization.Remote))
	}
	err = services.Records.Pull(accessOf(organization), orgClonePath)
	if err != nil {
		return proposal{}, err
	}
	catalogue, err := distill.LoadCatalogue(orgClonePath)
	if err != nil {
		return proposal{}, err
	}
	plan := proposal{
		ProposalPlan: ProposalPlan{Organization: organization.Slug, Level: level},
		organization: organization,
		orgClonePath: orgClonePath,
		levelID:      parsedLevel.ID(),
		report:       candidates.Report,
		filesByLevel: map[string]map[string]string{},
	}
	byLevel := map[string][]distill.Candidate{}
	for _, candidate := range groupCandidates(candidates.Files) {
		target, lifted, dropped, err := placeCandidate(candidate, level, candidates.Report, request, organization, orgClonePath)
		if err != nil {
			return proposal{}, err
		}
		if dropped != nil {
			plan.Dropped = append(plan.Dropped, *dropped)
			continue
		}
		if lifted != nil {
			plan.Lifted = append(plan.Lifted, *lifted)
			candidate.Files, err = declaration.AnnotateApplyIf(candidate.Kind, candidate.Files, lifted.ApplyIf)
			if err != nil {
				return proposal{}, fmt.Errorf("%s/%s: %w", candidate.Kind, candidate.ID, err)
			}
		}
		byLevel[target] = append(byLevel[target], candidate)
	}
	for _, target := range levelsInOrder(byLevel, level) {
		group := byLevel[target]
		distill.Compare(group, catalogue, target)
		levelPath := strings.ReplaceAll(target, ".", "/")
		for _, candidate := range group {
			summary := distill.Summarise(target, []distill.Candidate{candidate})[0]
			if candidate.Verdict == distill.VerdictIdentical {
				plan.LeftOut = append(plan.LeftOut, summary)
				continue
			}
			plan.Included = append(plan.Included, summary)
			if plan.filesByLevel[levelPath] == nil {
				plan.filesByLevel[levelPath] = map[string]string{}
			}
			for relative, content := range candidate.Files {
				plan.filesByLevel[levelPath][relative] = content
			}
		}
	}
	if len(plan.Included) == 0 {
		return proposal{}, refusal.New("nothing new to propose: every candidate is identical to a declaration the organisation has, or the judge dropped it")
	}
	err = lintCandidates(plan.filesByLevel)
	if err != nil {
		return proposal{}, err
	}
	for _, levelPath := range sortedKeys(plan.filesByLevel) {
		if !strings.HasPrefix(levelPath, address.Users+"/") {
			continue
		}
		userRecord, err := userRecordIfMissing(orgClonePath, levelPath, strings.TrimPrefix(levelPath, address.Users+"/"), organization, services)
		if err != nil {
			return proposal{}, err
		}
		for relative, content := range userRecord {
			plan.filesByLevel[levelPath][relative] = content
		}
	}
	return plan, nil
}

// levelsInOrder lists the levels candidates go to, the distilled level
// first and the others alphabetically.
func levelsInOrder(byLevel map[string][]distill.Candidate, level string) []string {
	var levels []string
	for target := range byLevel {
		if target != level {
			levels = append(levels, target)
		}
	}
	sort.Strings(levels)
	if _, present := byLevel[level]; present {
		levels = append([]string{level}, levels...)
	}
	return levels
}

// placeCandidate says where a candidate goes: the distilled level, unless
// the judge lifted it to another or dropped it, and the request did not ask
// for the candidates as distilled. A lift to the project level, or to the
// level it already has, keeps it where it is.
func placeCandidate(candidate distill.Candidate, level string, report *distill.Report, request ProposeRequest, organization machine.OrganizationRef, orgClonePath string) (target string, lifted *Placement, dropped *Placement, err error) {
	target = level
	if report == nil || request.AsDistilled {
		return target, nil, nil, nil
	}
	var judgement *distill.Judgement
	for _, summary := range report.Candidates {
		if summary.Kind == candidate.Kind && summary.ID == candidate.ID {
			judgement = summary.Judgement
		}
	}
	if judgement == nil {
		return target, nil, nil, nil
	}
	switch judgement.Suggestion {
	case "drop":
		reason := judgement.Reason
		if len(judgement.Related) > 0 {
			reason = "covered by " + strings.Join(judgement.Related, ", ") + ": " + reason
		}
		return "", nil, &Placement{Kind: candidate.Kind, ID: candidate.ID, From: level, Reason: reason}, nil
	case "lift":
		switch judgement.Level {
		case "organization":
			target = address.Organizations + "." + organization.Slug
		case "team":
			target, err = teamLevelOf(orgClonePath, level, organization.Slug, request.Team)
			if err != nil {
				return "", nil, nil, err
			}
		case "user":
			target = address.Users + "." + userSlugOf(organization)
		default:
			return level, nil, nil, nil
		}
		if target == level {
			return level, nil, nil, nil
		}
		var labels []string
		for _, label := range judgement.ApplyIf {
			if _, _, ok := declaration.SplitLabel(label); ok {
				labels = append(labels, label)
			}
		}
		return target, &Placement{Kind: candidate.Kind, ID: candidate.ID, From: level, To: target, ApplyIf: labels, Reason: judgement.Reason}, nil, nil
	}
	return target, nil, nil, nil
}

// teamLevelOf is the team level a lift goes to: the team named in the
// request, else the one team the distilled project belongs to; several
// teams need the request to say which.
func teamLevelOf(orgClonePath string, level string, organizationSlug string, requested string) (string, error) {
	teamLevel := func(team string) string {
		return address.Organizations + "." + organizationSlug + "." + address.Teams + "." + team
	}
	if requested != "" {
		return teamLevel(requested), nil
	}
	segments := strings.Split(level, ".")
	var teams []string
	switch {
	case len(segments) == 4 && segments[2] == address.Teams:
		teams = []string{segments[3]}
	case len(segments) == 4 && segments[2] == address.Projects:
		content, err := os.ReadFile(filepath.Join(orgClonePath, "declarations", filepath.FromSlash(strings.ReplaceAll(level, ".", "/")), "project.yaml"))
		if err == nil {
			project, _ := declaration.ParseProject(segments[3], content)
			teams = project.Teams
		}
	}
	switch len(teams) {
	case 1:
		return teamLevel(teams[0]), nil
	case 0:
		return "", refusal.New("the judge lifts a declaration to the team level; pass --team to say which team")
	default:
		return "", refusal.New(fmt.Sprintf("the judge lifts a declaration to the team level and the project belongs to the teams %s; pass --team to say which", strings.Join(teams, ", ")))
	}
}

// organizationOfLevel is the organisation a level belongs to: the one in
// its address, or for a user's level the one named or active.
func organizationOfLevel(services Services, level string, named string) (machine.OrganizationRef, string, error) {
	if strings.HasPrefix(level, address.Organizations+".") {
		slug := strings.Split(level, ".")[1]
		if named != "" && named != slug {
			return machine.OrganizationRef{}, "", refusal.New(fmt.Sprintf("the level %s belongs to %s, not %s", level, slug, named))
		}
		return services.Organizations.Lookup(slug)
	}
	return resolveOrganization(services, named)
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
		return nil, refusal.New(fmt.Sprintf("the level users.%s is not yours (%s); a user's level is proposed by that user", userSlug, who.Slug))
	}
	_, content, err := scaffold.UserRecord(who, services.Clock())
	if err != nil {
		return nil, err
	}
	return map[string]string{"user.yaml": content}, nil
}

// LevelOf is the level a summarised candidate sits at, read off its address.
func LevelOf(summary distill.CandidateSummary) string {
	return strings.TrimSuffix(summary.Address, "."+summary.Kind+"."+summary.ID)
}

// pullRequestBody is the comparison as the reviewer reads it: each
// candidate with its level, the judge's view, what was lifted or dropped on
// its advice, and what was left out.
func pullRequestBody(source string, plan proposal, asDistilled bool) string {
	var body strings.Builder
	fmt.Fprintf(&body, "Proposed by `rein propose` from `%s` to the level `%s`.\n\n", filepath.Base(source), plan.Level)
	judged := map[string]*distill.Judgement{}
	if plan.report != nil {
		for _, summary := range plan.report.Candidates {
			judged[summary.Kind+"/"+summary.ID] = summary.Judgement
		}
	}
	lifted := map[string]Placement{}
	for _, placement := range plan.Lifted {
		lifted[placement.Kind+"/"+placement.ID] = placement
	}
	fmt.Fprintf(&body, "| kind | id | level | compared with the organisation | the judge |\n|---|---|---|---|---|\n")
	for _, summary := range plan.Included {
		level := LevelOf(summary)
		if placement, moved := lifted[summary.Kind+"/"+summary.ID]; moved {
			level += " (lifted from " + placement.From
			if len(placement.ApplyIf) > 0 {
				level += ", applyIf " + strings.Join(placement.ApplyIf, " ")
			}
			level += ")"
		}
		fmt.Fprintf(&body, "| %s | %s | %s | %s | %s |\n", summary.Kind, summary.ID, level, describeVerdict(summary), describeJudgement(judged[summary.Kind+"/"+summary.ID]))
	}
	if plan.report != nil && plan.report.Judge != nil {
		fmt.Fprintf(&body, "\nJudge: %s, %s", plan.report.Judge.Backend, plan.report.Judge.Status)
		if len(plan.report.Judge.Problems) > 0 {
			fmt.Fprintf(&body, ": %s", strings.Join(plan.report.Judge.Problems, "; "))
		}
		fmt.Fprintf(&body, "\n")
	}
	if asDistilled && plan.report != nil {
		fmt.Fprintf(&body, "\nPlaced as distilled (`--as-distilled`); the judge's lifts and drops above are advice.\n")
	}
	if len(plan.Dropped) > 0 {
		fmt.Fprintf(&body, "\nDropped on the judge's advice, still in the distillation folder:\n")
		for _, placement := range plan.Dropped {
			fmt.Fprintf(&body, "- %s/%s: %s\n", placement.Kind, placement.ID, placement.Reason)
		}
	}
	if len(plan.LeftOut) > 0 {
		fmt.Fprintf(&body, "\nLeft out, identical to what the organisation has:\n")
		for _, summary := range plan.LeftOut {
			fmt.Fprintf(&body, "- %s/%s = %s\n", summary.Kind, summary.ID, strings.Join(summary.Related, ", "))
		}
	}
	if plan.report != nil {
		var notes []string
		for _, summary := range plan.report.Candidates {
			for _, note := range summary.Notes {
				notes = append(notes, fmt.Sprintf("%s/%s: %s", summary.Kind, summary.ID, note))
			}
		}
		if len(notes) > 0 {
			fmt.Fprintf(&body, "\nNotes from the distillation:\n")
			for _, note := range notes {
				fmt.Fprintf(&body, "- %s\n", note)
			}
		}
		if len(plan.report.Skipped) > 0 {
			fmt.Fprintf(&body, "\nNot distilled:\n")
			for _, skipped := range plan.report.Skipped {
				fmt.Fprintf(&body, "- %s\n", skipped)
			}
		}
	}
	fmt.Fprintf(&body, "\n`rein lint` passed on these files before the proposal; `rein review` judges the prose against the levels it meets.\n")
	return body.String()
}

// describeJudgement renders the judge's entry for the table, quotes included.
func describeJudgement(judgement *distill.Judgement) string {
	if judgement == nil {
		return "not judged"
	}
	text := judgement.Verdict
	if len(judgement.Related) > 0 {
		text += " with " + strings.Join(judgement.Related, ", ")
	}
	text += "; " + judgement.Suggestion
	if judgement.Suggestion == "lift" && judgement.Level != "" {
		text += " to the " + judgement.Level + " level"
	}
	text += ": " + judgement.Reason
	for _, quote := range judgement.Quotes {
		text += fmt.Sprintf(" (%s: “%s”)", quote.Address, quote.Text)
	}
	return strings.ReplaceAll(text, "|", "\\|")
}

func describeVerdict(summary distill.CandidateSummary) string {
	switch summary.Verdict {
	case distill.VerdictSameID:
		return "the same id exists at " + strings.Join(summary.Related, ", ") + "; resolution replaces or refuses it by enforcement"
	default:
		return "new"
	}
}
