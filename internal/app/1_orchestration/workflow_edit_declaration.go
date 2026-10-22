package orchestration

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Danidani23/agents-rein/internal/app/2_services/address"
	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// EditDeclarationRequest is what changes around a declaration: the level it
// lives at and its selectors. Its content stays with propose.
type EditDeclarationRequest struct {
	Organization string
	Address      string
	Level        string
	Selectors    declaration.Selectors
	Title        string
}

// EditDeclarationResult is what EditDeclaration did.
type EditDeclarationResult struct {
	Organization   string
	Address        string
	NewAddress     string
	Changed        []string
	Branch         string
	PullRequestURL string
}

// EditDeclaration moves a declaration to another level, or changes its
// selectors, as one pull request on the org repository: the files are
// written at the new level with the selectors set exactly, the old folder
// goes when the level changed, and the org clone ends on main either way.
func (orchestrator Orchestrator) EditDeclaration(request EditDeclarationRequest) (EditDeclarationResult, error) {
	services := orchestrator.Services
	located, err := locateDeclaration(services, request.Organization, request.Address)
	if err != nil {
		return EditDeclarationResult{}, err
	}
	targetLevel, err := address.Parse(request.Level)
	if err != nil || targetLevel.IsDeclaration() {
		return EditDeclarationResult{}, refusal.New(fmt.Sprintf("%q is not a level: organizations.<org>, organizations.<org>.teams.<team>, organizations.<org>.projects.<project> or organizations.<org>.users.<user>", request.Level))
	}
	if targetLevel.OrganizationSlug() != located.organization.Slug {
		return EditDeclarationResult{}, refusal.New(fmt.Sprintf("the level %s is not in %s", request.Level, located.organization.Slug))
	}
	accessToken, err := services.Credentials.AccessToken(located.organization.Login)
	if err != nil {
		return EditDeclarationResult{}, err
	}
	access := accessOf(located.organization)
	result := EditDeclarationResult{Organization: located.organization.Slug, Address: request.Address, NewAddress: request.Level + "." + located.kind + "." + located.id}
	targetLevelPath := targetLevel.Path()
	var currentFiles map[string]string
	var newFiles map[string]string
	var removed []string
	pushed := false
	err = runSteps(services.Progress, []Step{
		{
			Name: "pulling the org repository",
			Do:   func() error { return services.Records.Pull(access, located.orgClonePath) },
		},
		{
			Name: "checking the declaration and the level",
			Do: func() error {
				var current declaration.Selectors
				currentFiles, current, err = readDeclaration(located)
				if err != nil {
					return err
				}
				tree, err := services.Records.Tree(located.orgClonePath)
				if err != nil {
					return err
				}
				err = checkLevelExists(tree, located.organization, request.Level)
				if err != nil {
					return err
				}
				err = checkSelectors(tree.Labels, request.Selectors)
				if err != nil {
					return err
				}
				if request.Level != located.level {
					result.Changed = append(result.Changed, "level")
					_, _, err := readDeclaration(declarationLocation{orgClonePath: located.orgClonePath, level: request.Level, levelPath: targetLevelPath, kind: located.kind, id: located.id, organization: located.organization})
					if err == nil {
						return refusal.New(fmt.Sprintf("%s already exists; remove it first, or pick another level", result.NewAddress))
					}
				}
				if strings.Join(current.ApplyIf, ",") != strings.Join(request.Selectors.ApplyIf, ",") {
					result.Changed = append(result.Changed, "applyIf")
				}
				if strings.Join(current.DoNotApplyIf, ",") != strings.Join(request.Selectors.DoNotApplyIf, ",") {
					result.Changed = append(result.Changed, "doNotApplyIf")
				}
				if current.Enforcement != request.Selectors.Enforcement {
					result.Changed = append(result.Changed, "enforcement")
				}
				if len(result.Changed) == 0 {
					return refusal.New("nothing changed on " + request.Address)
				}
				newFiles, err = declaration.SetSelectors(located.kind, currentFiles, request.Selectors)
				return err
			},
		},
	})
	if err != nil {
		return EditDeclarationResult{}, err
	}
	result.Branch = fmt.Sprintf("rein/edit-%s-%s", located.id, services.Clock().UTC().Format("20060102-150405"))
	title := request.Title
	if title == "" {
		title = fmt.Sprintf("%s: %s", request.Address, describeDeclarationChange(result, request))
	}
	err = runSteps(services.Progress, []Step{
		{
			Name: "branching the org repository",
			Do:   func() error { return services.Proposals.Branch(located.orgClonePath, result.Branch) },
			Undo: func() error { return services.Proposals.Abandon(access, located.orgClonePath, result.Branch, pushed) },
		},
		{
			Name: "writing and pushing the change",
			Do: func() error {
				var paths []string
				if request.Level != located.level {
					for relative := range currentFiles {
						removed = append(removed, path.Join("declarations", located.levelPath, relative))
					}
					sort.Strings(removed)
					err := services.Proposals.Remove(located.orgClonePath, removed)
					if err != nil {
						return err
					}
					paths = append(paths, removed...)
				}
				written, err := services.Proposals.Write(located.orgClonePath, targetLevelPath, newFiles)
				if err != nil {
					return err
				}
				paths = append(paths, written...)
				if strings.HasPrefix(targetLevelPath, address.Users+"/") {
					userRecord, err := userRecordIfMissing(located.orgClonePath, targetLevelPath, path.Base(targetLevelPath), located.organization, services)
					if err != nil {
						return err
					}
					if len(userRecord) > 0 {
						recorded, err := services.Proposals.Write(located.orgClonePath, targetLevelPath, userRecord)
						if err != nil {
							return err
						}
						paths = append(paths, recorded...)
					}
				}
				err = services.Proposals.Publish(access, located.orgClonePath, result.Branch, paths, title+" (rein declaration edit)")
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
				body := fmt.Sprintf("Changed by `rein declaration edit`: %s.\n\n- level: `%s`\n- applyIf: %s\n- doNotApplyIf: %s\n- enforcement: %s\n", describeDeclarationChange(result, request), request.Level, orNone(strings.Join(request.Selectors.ApplyIf, " ")), orNone(strings.Join(request.Selectors.DoNotApplyIf, " ")), orNone(request.Selectors.Enforcement))
				if len(removed) > 0 {
					body += "\nRemoved from the old level:\n"
					for _, relativePath := range removed {
						body += "- " + relativePath + "\n"
					}
				}
				var err error
				result.PullRequestURL, err = services.Proposals.OpenPullRequest(accessToken, located.organization.Owner, located.organization.Repository, ports.PullRequest{Branch: result.Branch, Title: title, Body: body})
				return err
			},
		},
	})
	if err != nil {
		return EditDeclarationResult{}, err
	}
	err = services.Proposals.BackToMain(located.orgClonePath, result.Branch)
	if err != nil {
		return EditDeclarationResult{}, err
	}
	return result, nil
}

// declarationLocation is where a declaration lives in an org clone.
type declarationLocation struct {
	organization machine.OrganizationRef
	orgClonePath string
	level        string
	levelPath    string
	kind         string
	id           string
}

// locateDeclaration resolves an address to the organisation's clone and the
// declaration's place in it, without reading it.
func locateDeclaration(services Services, named string, text string) (declarationLocation, error) {
	parsed, err := address.Parse(text)
	if err != nil || !parsed.IsDeclaration() {
		return declarationLocation{}, refusal.New(fmt.Sprintf("%q is not a declaration's address, such as organizations.acme.skills.commit or organizations.acme.users.jane.rules.brief", text))
	}
	level, _ := parsed.Parent()
	organization, orgClonePath, err := organizationOfLevel(services, level.String(), named)
	if err != nil {
		return declarationLocation{}, err
	}
	return declarationLocation{organization: organization, orgClonePath: orgClonePath, level: level.String(), levelPath: level.Path(), kind: parsed.Kind(), id: parsed.ID()}, nil
}

// readDeclaration reads a declaration's files, keyed by their path inside
// the level, and its selectors; a declaration that is not there is a
// refusal.
func readDeclaration(located declarationLocation) (map[string]string, declaration.Selectors, error) {
	root := filepath.Join(located.orgClonePath, "declarations", filepath.FromSlash(located.levelPath))
	files := map[string]string{}
	switch located.kind {
	case address.Rules, address.Permissions, address.MCPServers:
		extension := ".md"
		if located.kind != address.Rules {
			extension = ".yaml"
		}
		relative := path.Join(located.kind, located.id+extension)
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err == nil {
			files[relative] = string(content)
		}
	case address.Skills, address.Hooks:
		folder := filepath.Join(root, located.kind, located.id)
		_ = filepath.WalkDir(folder, func(current string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil || entry.IsDir() {
				return nil
			}
			content, err := os.ReadFile(current)
			if err != nil {
				return err
			}
			relative, _ := filepath.Rel(root, current)
			files[filepath.ToSlash(relative)] = string(content)
			return nil
		})
	default:
		return nil, declaration.Selectors{}, refusal.New(fmt.Sprintf("%s is not a declaration kind: rules, skills, hooks, permissions or mcpservers", located.kind))
	}
	if len(files) == 0 {
		return nil, declaration.Selectors{}, refusal.New(fmt.Sprintf("no declaration at %s.%s.%s", located.level, located.kind, located.id))
	}
	selectors, problems := declaration.SelectorsOf(located.kind, files)
	for _, problem := range problems {
		if problem.Severity == declaration.SeverityError {
			return nil, declaration.Selectors{}, refusal.New(fmt.Sprintf("%s.%s.%s cannot be read: %s", located.level, located.kind, located.id, problem.Message))
		}
	}
	return files, selectors, nil
}

// checkLevelExists refuses a level the org repository does not have: a
// team or a project without a record; a user's level is the acting user's
// to create.
func checkLevelExists(tree declaration.Tree, organization machine.OrganizationRef, level string) error {
	segments := strings.Split(level, ".")
	switch {
	case len(segments) == 4 && segments[2] == address.Users:
		if segments[3] != userSlugOf(organization) {
			if _, found := tree.Users[segments[3]]; !found {
				return refusal.New(fmt.Sprintf("the level %s does not exist; a user's level is made by that user", level))
			}
		}
	case len(segments) == 4 && segments[2] == address.Teams:
		if _, found := tree.Teams[segments[3]]; !found {
			return refusal.New(fmt.Sprintf("team %q does not exist in %s; teams are %s", segments[3], organization.Slug, strings.Join(sortedKeys(tree.Teams), ", ")))
		}
	case len(segments) == 4 && segments[2] == address.Projects:
		if _, found := tree.Projects[segments[3]]; !found {
			return refusal.New(fmt.Sprintf("project %q does not exist in %s; projects are %s", segments[3], organization.Slug, strings.Join(sortedKeys(tree.Projects), ", ")))
		}
	}
	return nil
}

// checkSelectors refuses labels outside the registry, two values of a
// single-valued category, and an enforcement that is neither word.
func checkSelectors(registry map[string]declaration.LabelCategory, selectors declaration.Selectors) error {
	for _, label := range append(append([]string{}, selectors.ApplyIf...), selectors.DoNotApplyIf...) {
		category, value, ok := declaration.SplitLabel(label)
		if !ok {
			return refusal.New(fmt.Sprintf("label %q is not category:value", label))
		}
		registered, found := registry[category]
		if !found {
			return refusal.New(fmt.Sprintf("label %q names no category of the registry; categories are %s", label, strings.Join(sortedKeys(registry), ", ")))
		}
		if _, found := registered.Labels[value]; !found {
			return refusal.New(fmt.Sprintf("label %q is not in the registry; %s has %s", label, category, strings.Join(registered.LabelValues(), ", ")))
		}
	}
	conflicts := declaration.SingleValueConflicts(registry, selectors.ApplyIf)
	if len(conflicts) > 0 {
		return refusal.New("applyIf " + strings.Join(conflicts, "; "))
	}
	if selectors.Enforcement != "" && selectors.Enforcement != declaration.EnforcementRequired && selectors.Enforcement != declaration.EnforcementDefault {
		return refusal.New(fmt.Sprintf("enforcement %q is neither required nor default", selectors.Enforcement))
	}
	return nil
}

// describeDeclarationChange says what an edit does, for the title.
func describeDeclarationChange(result EditDeclarationResult, request EditDeclarationRequest) string {
	var parts []string
	for _, changed := range result.Changed {
		switch changed {
		case "level":
			parts = append(parts, "moved to "+request.Level)
		case "applyIf":
			parts = append(parts, "applyIf "+orNone(strings.Join(request.Selectors.ApplyIf, " ")))
		case "doNotApplyIf":
			parts = append(parts, "doNotApplyIf "+orNone(strings.Join(request.Selectors.DoNotApplyIf, " ")))
		case "enforcement":
			parts = append(parts, "enforcement "+orNone(request.Selectors.Enforcement))
		}
	}
	return strings.Join(parts, ", ")
}

func orNone(text string) string {
	if text == "" {
		return "none"
	}
	return text
}
