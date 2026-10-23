package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/address"
	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
	"github.com/Danidani23/agents-rein/internal/ports"
)

func newDeclarationCommand(environment *commandEnvironment) *cobra.Command {
	command := &cobra.Command{
		Use:   "declaration",
		Short: "List the declarations of an organisation, move one or change its labels, or remove one",
		Long: "A declaration is a rule, a skill, a hook, a permission set or a tool server at a level of the\n" +
			"org repository, addressed as organizations.acme.skills.commit or organizations.acme.users.jane.rules.brief.\n" +
			"Its content enters and changes through rein propose; what is around it changes here: the\n" +
			"level it lives at, the labels it applies to, its enforcement, and whether it exists. Every\n" +
			"change is a pull request on the org repository.",
	}
	command.AddCommand(newDeclarationListCommand(environment), newDeclarationEditCommand(environment), newDeclarationRemoveCommand(environment))
	return command
}

func newDeclarationListCommand(environment *commandEnvironment) *cobra.Command {
	var organizationSlug, level string
	command := &cobra.Command{
		Use:   "list",
		Short: "Show every declaration with its level, labels and enforcement",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			orchestrator, release, err := environment.orchestrateLocked()
			if err != nil {
				return err
			}
			defer release()
			err = requireLogin(orchestrator)
			if err != nil {
				return err
			}
			listed, err := orchestrator.ListDeclarations(organizationSlug, level)
			if err != nil {
				return err
			}
			if len(listed) == 0 {
				fmt.Fprintf(environment.out, "no declarations yet; rein distill and rein propose add them\n")
				return nil
			}
			rows := [][]string{{"ADDRESS", "APPLY IF", "DO NOT APPLY IF", "ENFORCEMENT"}}
			for _, described := range listed {
				rows = append(rows, []string{described.Address, strings.Join(described.Selectors.ApplyIf, " "), strings.Join(described.Selectors.DoNotApplyIf, " "), firstOf(described.Selectors.Enforcement, "default")})
			}
			printTable(environment, rows)
			return nil
		},
	}
	command.Flags().StringVar(&organizationSlug, "org", "", "the organisation's slug (default: the active one)")
	command.Flags().StringVar(&level, "level", "", "only the declarations at this level")
	return command
}

// printTable prints rows with aligned columns, the first row as the
// header, bold on a terminal.
func printTable(environment *commandEnvironment, rows [][]string) {
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for column, cell := range row {
			if len([]rune(cell)) > widths[column] {
				widths[column] = len([]rune(cell))
			}
		}
	}
	style := environment.style()
	for index, row := range rows {
		var cells []string
		for column, cell := range row {
			padded := cell + strings.Repeat(" ", widths[column]-len([]rune(cell)))
			if column == len(row)-1 {
				padded = cell
			}
			cells = append(cells, padded)
		}
		line := strings.TrimRight(strings.Join(cells, "  "), " ")
		if index == 0 {
			line = style.Bold(line)
		}
		fmt.Fprintln(environment.out, line)
	}
}

// describeSelectors renders selectors on one line.
func describeSelectors(selectors declaration.Selectors) string {
	var parts []string
	if len(selectors.ApplyIf) > 0 {
		parts = append(parts, "applyIf "+strings.Join(selectors.ApplyIf, " "))
	}
	if len(selectors.DoNotApplyIf) > 0 {
		parts = append(parts, "doNotApplyIf "+strings.Join(selectors.DoNotApplyIf, " "))
	}
	parts = append(parts, firstOf(selectors.Enforcement, "default"))
	return strings.Join(parts, ", ")
}

// declarationEditAnswers are the flags of declaration edit.
type declarationEditAnswers struct {
	organization string
	level        string
	applyIf      []string
	doNotApplyIf []string
	enforcement  string
	yes          bool
}

func newDeclarationEditCommand(environment *commandEnvironment) *cobra.Command {
	var answers declarationEditAnswers
	command := &cobra.Command{
		Use:   "edit <address>",
		Short: "Move a declaration to another level, or change the labels it applies to and its enforcement",
		Long: "edit changes what is around a declaration: the level it lives at, the labels it applies to\n" +
			"or must not apply to, and whether it is required or default. On a terminal it asks each with\n" +
			"the current value as the default; with flags only the given ones change. A new level carries\n" +
			"the files over and removes the old ones in the same pull request. The content stays as it is:\n" +
			"rein propose changes that.",
		Example: "  rein declaration edit organizations.acme.users.jane.skills.commit --level organizations.acme\n  rein declaration edit organizations.acme.skills.go-style --apply-if language:go --yes",
		Args:    cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			given := map[string]bool{}
			for _, name := range []string{"level", "apply-if", "do-not-apply-if", "enforcement"} {
				given[name] = command.Flags().Changed(name)
			}
			return runDeclarationEdit(environment, arguments[0], answers, given)
		},
	}
	command.Flags().StringVar(&answers.organization, "org", "", "the organisation's slug, for a user's level (default: the active one)")
	command.Flags().StringVar(&answers.level, "level", "", "the level to move it to")
	command.Flags().StringArrayVar(&answers.applyIf, "apply-if", nil, "a label the declaration applies to; repeat for several, replacing the current ones; pass \"\" for none")
	command.Flags().StringArrayVar(&answers.doNotApplyIf, "do-not-apply-if", nil, "a label the declaration must not apply to; repeat for several; pass \"\" for none")
	command.Flags().StringVar(&answers.enforcement, "enforcement", "", "required or default")
	command.Flags().BoolVar(&answers.yes, "yes", false, "do not ask for confirmation")
	return command
}

func runDeclarationEdit(environment *commandEnvironment, declarationAddress string, given declarationEditAnswers, flagsGiven map[string]bool) error {
	orchestrator, release, err := environment.orchestrateLocked()
	if err != nil {
		return err
	}
	defer release()
	err = requireLogin(orchestrator)
	if err != nil {
		return err
	}
	current, err := orchestrator.DescribeDeclaration(given.organization, declarationAddress)
	if err != nil {
		return err
	}
	organization, err := orchestrator.DescribeOrganization(current.Organization)
	if err != nil {
		return err
	}
	request := orchestration.EditDeclarationRequest{Organization: current.Organization, Address: declarationAddress, Level: current.Level, Selectors: current.Selectors}
	anyGiven := false
	for _, name := range []string{"level", "apply-if", "do-not-apply-if", "enforcement"} {
		anyGiven = anyGiven || flagsGiven[name]
	}
	if flagsGiven["level"] {
		request.Level = given.level
	}
	if flagsGiven["apply-if"] {
		request.Selectors.ApplyIf = withoutEmpty(given.applyIf)
	}
	if flagsGiven["do-not-apply-if"] {
		request.Selectors.DoNotApplyIf = withoutEmpty(given.doNotApplyIf)
	}
	if flagsGiven["enforcement"] {
		request.Selectors.Enforcement = given.enforcement
	}
	prompter := environment.prompter()
	if !anyGiven && !prompter.CanAsk() {
		return refusal.New("nothing to change; pass --level, --apply-if, --do-not-apply-if or --enforcement, or run on a terminal to be asked")
	}
	again := !anyGiven
	for {
		if again && prompter.CanAsk() {
			request, err = askDeclarationEdit(prompter, organization, request)
			if err != nil {
				return refusal.New(err.Error())
			}
		}
		fmt.Fprintf(environment.out, "This will open a pull request on the org repository of %s that puts %s at %s with %s.\n", current.Organization, declarationAddress, request.Level, describeSelectors(request.Selectors))
		if given.yes {
			break
		}
		decision, err := prompter.ConfirmOrEdit("Continue?", "The files are written at the level with the selectors set, the old ones removed when the level changed, on a branch of the organisation's clone; the pull request is reviewed by the code owners. Nothing reaches main here.")
		if err != nil {
			return refusal.New(err.Error())
		}
		if decision == ports.DecisionEdit {
			again = true
			continue
		}
		if decision == ports.DecisionNo {
			return refusal.New("nothing changed")
		}
		break
	}
	result, err := orchestrator.EditDeclaration(request)
	if err != nil {
		return err
	}
	fmt.Fprintf(environment.out, "changed %s: %s\n", result.Address, strings.Join(result.Changed, ", "))
	if result.NewAddress != result.Address {
		fmt.Fprintf(environment.out, "its address after the merge: %s\n", result.NewAddress)
	}
	style := environment.style()
	fmt.Fprintf(environment.out, "\npull request: %s\n", style.Bold(style.Green(result.PullRequestURL)))
	printNext(environment, "review and merge it, then rein sync in each clone it reaches")
	return nil
}

// askDeclarationEdit asks the level, the labels and the enforcement with
// the current values as the defaults.
func askDeclarationEdit(prompter ports.Prompter, organization orchestration.OrganizationDescription, request orchestration.EditDeclarationRequest) (orchestration.EditDeclarationRequest, error) {
	var err error
	levels := []ports.Choice{{Name: address.Organizations + "." + organization.Slug, Description: "the whole organisation"}}
	for _, team := range organization.Teams {
		levels = append(levels, ports.Choice{Name: address.Organizations + "." + organization.Slug + "." + address.Teams + "." + team.Slug, Description: "the team " + team.Name})
	}
	for _, project := range organization.Projects {
		levels = append(levels, ports.Choice{Name: address.Organizations + "." + organization.Slug + "." + address.Projects + "." + project.Slug, Description: "the project " + project.Name})
	}
	levels = append(levels, ports.Choice{Name: organization.UserLevel, Description: "your own user level"})
	known := false
	for _, level := range levels {
		known = known || level.Name == request.Level
	}
	if !known {
		levels = append(levels, ports.Choice{Name: request.Level, Description: "where it is now"})
	}
	request.Level, err = prompter.Choose(ports.Select{
		Title:       "Level",
		Description: "Where the declaration lives, and so how far it reaches.",
		Explanation: "A higher level reaches more projects: the organisation everyone, a team its projects, a project itself, a user that person everywhere. The content stays the same; only the reach changes.",
		Choices:     levels,
		Default:     request.Level,
	})
	if err != nil {
		return request, err
	}
	var choices []ports.Choice
	for _, category := range organization.LabelCategories {
		for _, value := range category.Values {
			choices = append(choices, ports.Choice{Name: category.Category + ":" + value.Value, Description: value.Description})
		}
	}
	if len(choices) > 0 {
		request.Selectors.ApplyIf, err = prompter.ChooseMany(ports.Select{
			Title:       "applyIf",
			Description: "The labels a project must carry for the declaration to apply; several separated by commas; Enter for none, which means every project.",
			Choices:     choices,
			Default:     strings.Join(request.Selectors.ApplyIf, ","),
			Optional:    true,
		})
		if err != nil {
			return request, err
		}
		request.Selectors.DoNotApplyIf, err = prompter.ChooseMany(ports.Select{
			Title:       "doNotApplyIf",
			Description: "The labels that keep the declaration away from a project; several separated by commas; Enter for none.",
			Choices:     choices,
			Default:     strings.Join(request.Selectors.DoNotApplyIf, ","),
			Optional:    true,
		})
		if err != nil {
			return request, err
		}
	}
	request.Selectors.Enforcement, err = prompter.Choose(ports.Select{
		Title:       "Enforcement",
		Description: "Whether a level below may override the declaration.",
		Explanation: "A default declaration may be replaced by a lower level's own; a required one holds at every level below, and only an exception granted at its level lets a team or a project skip it.",
		Choices:     []ports.Choice{{Name: declaration.EnforcementDefault, Description: "a lower level may override it"}, {Name: declaration.EnforcementRequired, Description: "holds at every level below"}},
		Default:     firstOf(request.Selectors.Enforcement, declaration.EnforcementDefault),
	})
	if err != nil {
		return request, err
	}
	if request.Selectors.Enforcement == declaration.EnforcementDefault {
		request.Selectors.Enforcement = ""
	}
	return request, nil
}

// withoutEmpty drops empty entries, so --apply-if "" means none.
func withoutEmpty(items []string) []string {
	var kept []string
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			kept = append(kept, item)
		}
	}
	return kept
}

func newDeclarationRemoveCommand(environment *commandEnvironment) *cobra.Command {
	var organizationSlug string
	var yes bool
	command := &cobra.Command{
		Use:   "remove <address>",
		Short: "Open the pull request that takes a declaration out of the org repository",
		Long: "remove deletes the declaration's files on a branch of the organisation's clone and opens the\n" +
			"pull request. A declaration a skip list or an exception still names stays, since the\n" +
			"reference would dangle; take those out first.",
		Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			orchestrator, release, err := environment.orchestrateLocked()
			if err != nil {
				return err
			}
			defer release()
			err = requireLogin(orchestrator)
			if err != nil {
				return err
			}
			current, err := orchestrator.DescribeDeclaration(organizationSlug, arguments[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(environment.out, "This will open a pull request on the org repository of %s that removes %s: %s.\n", current.Organization, arguments[0], strings.Join(current.Files, ", "))
			if !yes {
				prompter := environment.prompter()
				if !prompter.CanAsk() {
					return refusal.New("no terminal to confirm on; pass --yes")
				}
				confirmed, err := prompter.Confirm("Continue?", "The files are removed on a branch; the pull request is reviewed by the code owners. Clones that received the declaration lose it at their next sync after the merge.")
				if err != nil {
					return refusal.New(err.Error())
				}
				if !confirmed {
					return refusal.New("nothing changed")
				}
			}
			result, err := orchestrator.RemoveDeclaration(orchestration.RemoveDeclarationRequest{Organization: organizationSlug, Address: arguments[0]})
			if err != nil {
				return err
			}
			style := environment.style()
			fmt.Fprintf(environment.out, "removed %s on branch %s: %s\n\npull request: %s\n", result.Address, result.Branch, strings.Join(result.Removed, ", "), style.Bold(style.Green(result.PullRequestURL)))
			printNext(environment, "review and merge it, then rein sync in each clone it reached")
			return nil
		},
	}
	command.Flags().StringVar(&organizationSlug, "org", "", "the organisation's slug, for a user's level (default: the active one)")
	command.Flags().BoolVar(&yes, "yes", false, "do not ask for confirmation")
	return command
}
