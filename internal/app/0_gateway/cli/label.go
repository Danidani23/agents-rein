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

const newCategoryChoice = "a new category"

func newLabelCommand(environment *commandEnvironment) *cobra.Command {
	label := &cobra.Command{
		Use:   "label",
		Short: "List and change the label registry of an organisation",
		Long: "The label registry says what a project can be: categories such as kind or language, each\n" +
			"with its values, and whether every project must carry one of them. Declarations apply to\n" +
			"projects by these labels. label list shows the registry; label add, remove and require\n" +
			"change one category in the organisation's clone and push it to main, as the record\n" +
			"creators do.",
	}
	label.AddCommand(newLabelListCommand(environment), newLabelAddCommand(environment), newLabelRemoveCommand(environment), newLabelRequireCommand(environment))
	return label
}

func newLabelListCommand(environment *commandEnvironment) *cobra.Command {
	var organizationSlug string
	command := &cobra.Command{
		Use:   "list",
		Short: "Show the registry: every category, whether it is required, and its values",
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
			organization, err := orchestrator.DescribeOrganization(organizationSlug)
			if err != nil {
				return err
			}
			fmt.Fprintf(environment.out, "label registry of %s\n", organization.Slug)
			for _, category := range organization.LabelCategories {
				line := category.Category
				if category.Required {
					line += "  required: every project carries one"
				}
				fmt.Fprintln(environment.out, line)
				for _, value := range category.Values {
					fmt.Fprintf(environment.out, "  %-16s %s\n", value.Value, value.Description)
				}
			}
			return nil
		},
	}
	command.Flags().StringVar(&organizationSlug, "org", "", "the organisation's slug (default: the active one)")
	return command
}

func newLabelAddCommand(environment *commandEnvironment) *cobra.Command {
	var request orchestration.LabelRegistryRequest
	var yes bool
	command := &cobra.Command{
		Use:   "add [category:value]",
		Short: "Add a value to a category, or a new category with its first value",
		Long: "add puts category:value into the registry with its description. A category that does not\n" +
			"exist yet is created; --required makes every project carry one of its values from then\n" +
			"on. Without the argument it asks on a terminal.",
		Example: "  rein label add\n  rein label add kind:worker --description \"a background worker\" --yes\n  rein label add stack:aws --description \"runs on AWS\" --required --yes",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			request.Operation = orchestration.LabelAdd
			return runLabelAdd(environment, arguments, request, yes)
		},
	}
	command.Flags().StringVar(&request.Organization, "org", "", "the organisation's slug (default: the active one)")
	command.Flags().StringVar(&request.Description, "description", "", "what the value means, one line")
	command.Flags().BoolVar(&request.Required, "required", false, "when the category is new: every project must carry one of its values")
	command.Flags().BoolVar(&yes, "yes", false, "do not ask for confirmation")
	return command
}

func runLabelAdd(environment *commandEnvironment, arguments []string, request orchestration.LabelRegistryRequest, yes bool) error {
	orchestrator, release, err := environment.orchestrateLocked()
	if err != nil {
		return err
	}
	defer release()
	err = requireLogin(orchestrator)
	if err != nil {
		return err
	}
	organization, err := orchestrator.DescribeOrganization(request.Organization)
	if err != nil {
		return err
	}
	request.Organization = organization.Slug
	prompter := environment.prompter()
	if len(arguments) == 1 {
		category, value, ok := declaration.SplitLabel(arguments[0])
		if !ok {
			return refusal.New(fmt.Sprintf("%q is not category:value, both lowercase letters, digits and single hyphens", arguments[0]))
		}
		request.Category, request.Value = category, value
	} else {
		if !prompter.CanAsk() {
			return refusal.New("name the label as category:value, or run on a terminal to be asked")
		}
		request, err = askLabelAdd(prompter, organization, request)
		if err != nil {
			return refusal.New(err.Error())
		}
	}
	exists := false
	for _, category := range organization.LabelCategories {
		if category.Category == request.Category {
			exists = true
		}
	}
	description := ""
	if request.Description != "" {
		description = fmt.Sprintf(" (%s)", request.Description)
	}
	if exists {
		fmt.Fprintf(environment.out, "This will add %s:%s%s to the registry of %s and push it to main.\n", request.Category, request.Value, description, organization.Slug)
	} else {
		required := "optional"
		if request.Required {
			required = "required: every project carries one of its values"
		}
		fmt.Fprintf(environment.out, "This will create the category %s in the registry of %s, %s, with its first value %s%s, and push it to main.\n", request.Category, organization.Slug, required, request.Value, description)
	}
	if !yes {
		if !prompter.CanAsk() {
			return refusal.New("no terminal to confirm on; pass --yes")
		}
		confirmed, err := prompter.Confirm("Continue?", "The registry file is written in the organisation's clone, committed and pushed. If the push is rejected, the clone is put back as the remote has it and nothing changes.")
		if err != nil {
			return refusal.New(err.Error())
		}
		if !confirmed {
			return refusal.New("nothing changed")
		}
	}
	result, err := orchestrator.EditLabelRegistry(request)
	if err != nil {
		return err
	}
	if result.CreatedCategory {
		fmt.Fprintf(environment.out, "created the category %s with %s:%s in %s\n", result.Category, result.Category, result.Value, result.Organization)
	} else {
		fmt.Fprintf(environment.out, "added %s:%s to the registry of %s\n", result.Category, result.Value, result.Organization)
	}
	fmt.Fprintf(environment.out, "registry: %s, pushed to main\n", result.RelativePath)
	return nil
}

// askLabelAdd asks the category, the value, its description and, for a new
// category, whether every project must carry one.
func askLabelAdd(prompter ports.Prompter, organization orchestration.OrganizationDescription, request orchestration.LabelRegistryRequest) (orchestration.LabelRegistryRequest, error) {
	var choices []ports.Choice
	for _, category := range organization.LabelCategories {
		description := fmt.Sprintf("%d values", len(category.Values))
		if category.Required {
			description += ", required"
		}
		choices = append(choices, ports.Choice{Name: category.Category, Description: description})
	}
	choices = append(choices, ports.Choice{Name: newCategoryChoice, Description: "name it next"})
	picked, err := prompter.Choose(ports.Select{
		Title:       "Category",
		Description: "The category the value belongs to.",
		Explanation: "A category groups values a project picks from, such as language or kind. Declarations apply by category:value, so a new category is a new way to say which projects a declaration is for.",
		Choices:     choices,
	})
	if err != nil {
		return request, err
	}
	request.Category = picked
	created := picked == newCategoryChoice
	if created {
		request.Category, err = prompter.Ask(ports.Text{
			Title:       "Category name",
			Description: "Lowercase letters, digits and single hyphens, such as stack or compliance.",
			Validate:    slugReason,
		})
		if err != nil {
			return request, err
		}
	}
	request.Value, err = prompter.Ask(ports.Text{
		Title:       "Value",
		Description: "The new value of " + request.Category + ", lowercase letters, digits and single hyphens.",
		Validate:    slugReason,
	})
	if err != nil {
		return request, err
	}
	request.Description, err = prompter.Ask(ports.Text{
		Title:       "Description",
		Description: "What the value means, one line; Enter for none.",
		Optional:    true,
	})
	if err != nil {
		return request, err
	}
	if created {
		request.Required, err = prompter.Confirm("Must every project carry one value of "+request.Category+"?", "A required category is asked first when a project is created or edited, and at least one value must be picked; the lint warns about a project that has none. An optional category is offered with the other labels, Enter for none.")
		if err != nil {
			return request, err
		}
	}
	return request, nil
}

// slugReason is the validation of a slug answer.
func slugReason(answer string) string {
	if address.IsSlug(answer) {
		return ""
	}
	return "lowercase letters, digits and single hyphens only, with no hyphen at either end"
}

func newLabelRemoveCommand(environment *commandEnvironment) *cobra.Command {
	var request orchestration.LabelRegistryRequest
	var yes bool
	command := &cobra.Command{
		Use:   "remove <category:value>",
		Short: "Take a value out of the registry; a value a project carries, or the last of its category, stays",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			request.Operation = orchestration.LabelRemove
			category, value, ok := declaration.SplitLabel(arguments[0])
			if !ok {
				return refusal.New(fmt.Sprintf("%q is not category:value", arguments[0]))
			}
			request.Category, request.Value = category, value
			return runLabelChange(environment, request, yes, fmt.Sprintf("This will remove %s:%s from the registry", category, value))
		},
	}
	command.Flags().StringVar(&request.Organization, "org", "", "the organisation's slug (default: the active one)")
	command.Flags().BoolVar(&yes, "yes", false, "do not ask for confirmation")
	return command
}

func newLabelRequireCommand(environment *commandEnvironment) *cobra.Command {
	var request orchestration.LabelRegistryRequest
	var yes, off bool
	command := &cobra.Command{
		Use:   "require <category>",
		Short: "Make every project carry one value of the category, or with --off make it optional again",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			request.Category = arguments[0]
			request.Operation = orchestration.LabelRequire
			summary := fmt.Sprintf("This will make the category %s required, so every project created or edited from now on carries one of its values,", arguments[0])
			if off {
				request.Operation = orchestration.LabelOptional
				summary = fmt.Sprintf("This will make the category %s optional,", arguments[0])
			}
			return runLabelChange(environment, request, yes, summary)
		},
	}
	command.Flags().StringVar(&request.Organization, "org", "", "the organisation's slug (default: the active one)")
	command.Flags().BoolVar(&off, "off", false, "make the category optional instead")
	command.Flags().BoolVar(&yes, "yes", false, "do not ask for confirmation")
	return command
}

// runLabelChange confirms and runs a removal or a required change.
func runLabelChange(environment *commandEnvironment, request orchestration.LabelRegistryRequest, yes bool, summary string) error {
	orchestrator, release, err := environment.orchestrateLocked()
	if err != nil {
		return err
	}
	defer release()
	err = requireLogin(orchestrator)
	if err != nil {
		return err
	}
	organization, err := orchestrator.DescribeOrganization(request.Organization)
	if err != nil {
		return err
	}
	request.Organization = organization.Slug
	fmt.Fprintf(environment.out, "%s in %s, and push the registry to main.\n", strings.TrimSuffix(summary, ","), organization.Slug)
	if !yes {
		prompter := environment.prompter()
		if !prompter.CanAsk() {
			return refusal.New("no terminal to confirm on; pass --yes")
		}
		confirmed, err := prompter.Confirm("Continue?", "The registry file is written in the organisation's clone, committed and pushed. If the push is rejected, the clone is put back as the remote has it and nothing changes.")
		if err != nil {
			return refusal.New(err.Error())
		}
		if !confirmed {
			return refusal.New("nothing changed")
		}
	}
	result, err := orchestrator.EditLabelRegistry(request)
	if err != nil {
		return err
	}
	switch request.Operation {
	case orchestration.LabelRemove:
		fmt.Fprintf(environment.out, "removed %s:%s from the registry of %s\n", result.Category, result.Value, result.Organization)
	case orchestration.LabelRequire:
		fmt.Fprintf(environment.out, "the category %s is required in %s\n", result.Category, result.Organization)
	default:
		fmt.Fprintf(environment.out, "the category %s is optional in %s\n", result.Category, result.Organization)
	}
	fmt.Fprintf(environment.out, "registry: %s, pushed to main\n", result.RelativePath)
	return nil
}
