package orchestration

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Danidani23/agents-rein/internal/app/2_services/address"
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
}

// ProposeResult is what ProposeDeclarations did.
type ProposeResult struct {
	Organization   string
	Level          string
	Branch         string
	PullRequestURL string
	Included       []distill.CandidateSummary
	LeftOut        []distill.CandidateSummary
}

// ProposeDeclarations lints the candidates, compares them with the
// organisation, leaves out the ones it already has, writes the rest on a
// branch of the org clone, pushes it and opens the pull request, with the
// comparison as its body. The org clone ends on main either way.
func (orchestrator Orchestrator) ProposeDeclarations(request ProposeRequest) (ProposeResult, error) {
	services := orchestrator.Services
	candidates, err := services.Proposals.Read(request.Source)
	if err != nil {
		return ProposeResult{}, refusal.New(fmt.Sprintf("cannot read %s: %v", request.Source, err))
	}
	if len(candidates.Files) == 0 {
		return ProposeResult{}, refusal.New(fmt.Sprintf("%s holds no declarations", request.Source))
	}
	level := request.Level
	if level == "" && candidates.Report != nil {
		level = candidates.Report.Level
	}
	if level == "" {
		organization, _, err := resolveOrganization(services, request.Organization)
		if err != nil {
			return ProposeResult{}, err
		}
		level = "users." + userSlugOf(organization)
	}
	parsedLevel, err := address.Parse(level)
	if err != nil || parsedLevel.IsDeclaration() {
		return ProposeResult{}, refusal.New(fmt.Sprintf("%q is not a level: organizations.<org>, organizations.<org>.teams.<team>, organizations.<org>.projects.<project> or users.<user>", level))
	}
	organization, orgClonePath, err := organizationOfLevel(services, level, request.Organization)
	if err != nil {
		return ProposeResult{}, err
	}
	if organization.Owner == "" || organization.Repository == "" {
		return ProposeResult{}, refusal.New(fmt.Sprintf("the org repository of %s is not on GitHub (%s); a pull request needs one", organization.Slug, organization.Remote))
	}
	accessToken, err := services.Credentials.AccessToken(organization.Login)
	if err != nil {
		return ProposeResult{}, err
	}
	access := accessOf(organization)
	levelPath := strings.ReplaceAll(level, ".", "/")
	grouped := groupCandidates(candidates.Files)
	err = lintCandidates(levelPath, candidates.Files)
	if err != nil {
		return ProposeResult{}, err
	}
	err = services.Records.Pull(access, orgClonePath)
	if err != nil {
		return ProposeResult{}, err
	}
	catalogue, err := distill.LoadCatalogue(orgClonePath)
	if err != nil {
		return ProposeResult{}, err
	}
	distill.Compare(grouped, catalogue, level)
	files := map[string]string{}
	result := ProposeResult{Organization: organization.Slug, Level: level}
	for _, candidate := range grouped {
		summary := distill.Summarise(level, []distill.Candidate{candidate})[0]
		if candidate.Verdict == distill.VerdictIdentical {
			result.LeftOut = append(result.LeftOut, summary)
			continue
		}
		result.Included = append(result.Included, summary)
		for relative, content := range candidate.Files {
			files[relative] = content
		}
	}
	if len(result.Included) == 0 {
		return ProposeResult{}, refusal.New("nothing new to propose: every candidate is identical to a declaration the organisation has")
	}
	if strings.HasPrefix(level, "users.") {
		userRecord, err := userRecordIfMissing(orgClonePath, levelPath, strings.TrimPrefix(level, "users."), organization, services)
		if err != nil {
			return ProposeResult{}, err
		}
		for relative, content := range userRecord {
			files[relative] = content
		}
	}
	result.Branch = fmt.Sprintf("rein/propose-%s-%s", parsedLevel.ID(), services.Clock().UTC().Format("20060102-150405"))
	title := request.Title
	if title == "" {
		title = fmt.Sprintf("Propose %d declarations to %s", len(result.Included), level)
	}
	body := pullRequestBody(request.Source, level, result, candidates.Report)
	pushed := false
	err = runSteps(services.Progress, []Step{
		{
			Name: "branching the org repository",
			Do:   func() error { return services.Proposals.Branch(orgClonePath, result.Branch) },
			Undo: func() error { return services.Proposals.Abandon(access, orgClonePath, result.Branch, pushed) },
		},
		{
			Name: "writing and pushing the proposal",
			Do: func() error {
				relativePaths, err := services.Proposals.Write(orgClonePath, levelPath, files)
				if err != nil {
					return err
				}
				err = services.Proposals.Publish(access, orgClonePath, result.Branch, relativePaths, title+" (rein propose)")
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
				result.PullRequestURL, err = services.Proposals.OpenPullRequest(accessToken, organization.Owner, organization.Repository, ports.PullRequest{Branch: result.Branch, Title: title, Body: body})
				return err
			},
		},
	})
	if err != nil {
		return ProposeResult{}, err
	}
	err = services.Proposals.BackToMain(orgClonePath, result.Branch)
	if err != nil {
		return ProposeResult{}, err
	}
	return result, nil
}

// organizationOfLevel is the organisation a level belongs to: the one in
// its address, or for a user's level the one named or active.
func organizationOfLevel(services Services, level string, named string) (machine.OrganizationRef, string, error) {
	if strings.HasPrefix(level, "organizations.") {
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

// lintCandidates writes the files under a scratch declarations tree at the
// level and refuses on any error the lint finds.
func lintCandidates(levelPath string, files map[string]string) error {
	scratch, err := os.MkdirTemp("", "rein-propose-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)
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

// pullRequestBody is the comparison as the reviewer reads it.
func pullRequestBody(source string, level string, result ProposeResult, report *distill.Report) string {
	var body strings.Builder
	fmt.Fprintf(&body, "Proposed by `rein propose` from `%s` to the level `%s`.\n\n", filepath.Base(source), level)
	judged := map[string]*distill.Judgement{}
	if report != nil {
		for _, summary := range report.Candidates {
			judged[summary.Kind+"/"+summary.ID] = summary.Judgement
		}
	}
	fmt.Fprintf(&body, "| kind | id | compared with the organisation | the judge |\n|---|---|---|---|\n")
	for _, summary := range result.Included {
		fmt.Fprintf(&body, "| %s | %s | %s | %s |\n", summary.Kind, summary.ID, describeVerdict(summary), describeJudgement(judged[summary.Kind+"/"+summary.ID]))
	}
	if report != nil && report.Judge != nil {
		fmt.Fprintf(&body, "\nJudge: %s, %s", report.Judge.Backend, report.Judge.Status)
		if len(report.Judge.Problems) > 0 {
			fmt.Fprintf(&body, ": %s", strings.Join(report.Judge.Problems, "; "))
		}
		fmt.Fprintf(&body, "\n")
	}
	if len(result.LeftOut) > 0 {
		fmt.Fprintf(&body, "\nLeft out, identical to what the organisation has:\n")
		for _, summary := range result.LeftOut {
			fmt.Fprintf(&body, "- %s/%s = %s\n", summary.Kind, summary.ID, strings.Join(summary.Related, ", "))
		}
	}
	if report != nil {
		var notes []string
		for _, summary := range report.Candidates {
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
		if len(report.Skipped) > 0 {
			fmt.Fprintf(&body, "\nNot distilled:\n")
			for _, skipped := range report.Skipped {
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
