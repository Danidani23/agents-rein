package orchestration

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// RemoveDeclarationRequest names the declaration to take out of the org
// repository.
type RemoveDeclarationRequest struct {
	Organization string
	Address      string
	Title        string
}

// RemoveDeclarationResult is what RemoveDeclaration did.
type RemoveDeclarationResult struct {
	Organization   string
	Address        string
	Removed        []string
	Branch         string
	PullRequestURL string
}

// RemoveDeclaration deletes a declaration's files on a branch of the org
// clone and opens the pull request. A declaration a skip list or an
// exception still names stays, since the reference would dangle.
func (orchestrator Orchestrator) RemoveDeclaration(request RemoveDeclarationRequest) (RemoveDeclarationResult, error) {
	services := orchestrator.Services
	located, err := locateDeclaration(services, request.Organization, request.Address)
	if err != nil {
		return RemoveDeclarationResult{}, err
	}
	accessToken, err := services.Credentials.AccessToken(located.organization.Login)
	if err != nil {
		return RemoveDeclarationResult{}, err
	}
	access := accessOf(located.organization)
	result := RemoveDeclarationResult{Organization: located.organization.Slug, Address: request.Address}
	pushed := false
	err = runSteps(services.Progress, []Step{
		{
			Name: "pulling the org repository",
			Do:   func() error { return services.Records.Pull(access, located.orgClonePath) },
		},
		{
			Name: "checking the declaration and what names it",
			Do: func() error {
				files, _, err := readDeclaration(located)
				if err != nil {
					return err
				}
				for relative := range files {
					result.Removed = append(result.Removed, path.Join("declarations", located.levelPath, relative))
				}
				sort.Strings(result.Removed)
				tree, err := services.Records.Tree(located.orgClonePath)
				if err != nil {
					return err
				}
				return checkNothingNames(tree, request.Address)
			},
		},
	})
	if err != nil {
		return RemoveDeclarationResult{}, err
	}
	result.Branch = fmt.Sprintf("rein/remove-%s-%s", located.id, services.Clock().UTC().Format("20060102-150405"))
	title := request.Title
	if title == "" {
		title = fmt.Sprintf("%s: removed", request.Address)
	}
	err = runSteps(services.Progress, []Step{
		{
			Name: "branching the org repository",
			Do:   func() error { return services.Proposals.Branch(located.orgClonePath, result.Branch) },
			Undo: func() error { return services.Proposals.Abandon(access, located.orgClonePath, result.Branch, pushed) },
		},
		{
			Name: "removing and pushing",
			Do: func() error {
				err := services.Proposals.Remove(located.orgClonePath, result.Removed)
				if err != nil {
					return err
				}
				err = services.Proposals.Publish(access, located.orgClonePath, result.Branch, result.Removed, title+" (rein declaration remove)")
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
				body := fmt.Sprintf("Removed by `rein declaration remove`: `%s`.\n\n", request.Address)
				for _, relativePath := range result.Removed {
					body += "- " + relativePath + "\n"
				}
				var err error
				result.PullRequestURL, err = services.Proposals.OpenPullRequest(accessToken, located.organization.Owner, located.organization.Repository, ports.PullRequest{Branch: result.Branch, Title: title, Body: body})
				return err
			},
		},
	})
	if err != nil {
		return RemoveDeclarationResult{}, err
	}
	err = services.Proposals.BackToMain(located.orgClonePath, result.Branch)
	if err != nil {
		return RemoveDeclarationResult{}, err
	}
	return result, nil
}

// checkNothingNames refuses the removal while a skip list or an exception
// names the declaration.
func checkNothingNames(tree declaration.Tree, declarationAddress string) error {
	var holders []string
	for _, slug := range sortedKeys(tree.Teams) {
		if contains(tree.Teams[slug].Skip, declarationAddress) {
			holders = append(holders, "the skip list of team "+slug)
		}
	}
	for _, slug := range sortedKeys(tree.Projects) {
		if contains(tree.Projects[slug].Skip, declarationAddress) {
			holders = append(holders, "the skip list of project "+slug)
		}
	}
	for _, exception := range tree.Exceptions {
		if exception.Declaration == declarationAddress {
			holders = append(holders, "the exception "+exception.Address)
		}
	}
	if len(holders) > 0 {
		return refusal.New(fmt.Sprintf("%s is named by %s; take those out first", declarationAddress, strings.Join(holders, ", ")))
	}
	return nil
}

func contains(items []string, item string) bool {
	for _, candidate := range items {
		if candidate == item {
			return true
		}
	}
	return false
}
