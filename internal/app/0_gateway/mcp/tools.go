package mcp

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"path/filepath"

	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/lint"
)

// Tools names the tools, in the order the agent is told about them.
var Tools = []string{"status", "resolve", "declaration_list", "declaration_describe", "organization_describe", "proposals_pending", "lint"}

type noArguments struct{}

type organizationArguments struct {
	Organization string `json:"organization,omitempty" jsonschema:"the organisation's slug; the active one when empty"`
}

type resolveArguments struct {
	Project      string `json:"project" jsonschema:"the project's slug"`
	Organization string `json:"organization,omitempty" jsonschema:"the organisation's slug; the active one when empty"`
}

type listArguments struct {
	Organization string `json:"organization,omitempty" jsonschema:"the organisation's slug; the active one when empty"`
	Level        string `json:"level,omitempty" jsonschema:"the address of one level: organizations.<org>, organizations.<org>.teams.<team>, organizations.<org>.projects.<project> or organizations.<org>.users.<user>; every level when empty"`
}

type describeArguments struct {
	Address      string `json:"address" jsonschema:"the declaration's address: its level's address followed by .<kind>.<id>, as declaration_list prints it"`
	Organization string `json:"organization,omitempty" jsonschema:"the organisation's slug; the active one when empty"`
}

type pendingArguments struct {
	Directory string `json:"directory,omitempty" jsonschema:"a folder inside a project clone, whose pending proposals are listed; the home's pending proposals when empty"`
}

type lintArguments struct {
	Path string `json:"path" jsonschema:"the declarations folder to lint"`
}

// addTools registers every tool: a query each, read-only, behind the login
// gate where its command is.
func addTools(server *mcp.Server, orchestrator orchestration.Orchestrator, folder string) {
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true}
	mcp.AddTool(server, &mcp.Tool{Name: "status", Title: "What rein knows about this machine", Annotations: readOnly,
		Description: "The logins, the agents found here, the connected organisations, the initialised project clones with the drift of what sync wrote and what became of their latest snapshot, and the pending proposals. Runs without a login."},
		query(func(noArguments) (any, error) { return result(orchestrator.Status()) }))
	mcp.AddTool(server, &mcp.Tool{Name: "resolve", Title: "What a project resolves and why", Annotations: readOnly,
		Description: "The declarations that apply to a project after the labels, the skip lists and the settling across levels, the way rein sync would write them. A problem means sync would refuse."},
		query(func(arguments resolveArguments) (any, error) {
			err := requireLogin(orchestrator)
			if err != nil {
				return nil, err
			}
			return result(orchestrator.ResolveProject(arguments.Organization, arguments.Project, ""))
		}))
	mcp.AddTool(server, &mcp.Tool{Name: "declaration_list", Title: "The declarations of an organisation", Annotations: readOnly,
		Description: "Every declaration in the organisation's clone, with its address, level, kind, id, selectors and files, sorted by address; one level's when a level is given."},
		query(func(arguments listArguments) (any, error) {
			err := requireLogin(orchestrator)
			if err != nil {
				return nil, err
			}
			return result(orchestrator.ListDeclarations(arguments.Organization, arguments.Level))
		}))
	mcp.AddTool(server, &mcp.Tool{Name: "declaration_describe", Title: "One declaration", Annotations: readOnly,
		Description: "What is around one declaration: its level, kind, id, selectors and the names of its files."},
		query(func(arguments describeArguments) (any, error) {
			err := requireLogin(orchestrator)
			if err != nil {
				return nil, err
			}
			return result(orchestrator.DescribeDeclaration(arguments.Organization, arguments.Address))
		}))
	mcp.AddTool(server, &mcp.Tool{Name: "organization_describe", Title: "An organisation as its clone holds it", Annotations: readOnly,
		Description: "The organisation's record, its teams with their projects, its projects, its user level and the label registry, read from the clone on this machine."},
		query(func(arguments organizationArguments) (any, error) {
			err := requireLogin(orchestrator)
			if err != nil {
				return nil, err
			}
			return result(orchestrator.DescribeOrganization(arguments.Organization))
		}))
	mcp.AddTool(server, &mcp.Tool{Name: "proposals_pending", Title: "The proposals waiting to be proposed", Annotations: readOnly,
		Description: "The proposal files rein distill wrote and rein propose has not opened a pull request from yet, with their level, candidates and time."},
		query(func(arguments pendingArguments) (any, error) {
			directory := arguments.Directory
			if directory != "" {
				directory = absoluteIn(folder, directory)
			}
			return result(orchestrator.PendingProposals(directory))
		}))
	mcp.AddTool(server, &mcp.Tool{Name: "lint", Title: "Lint a declarations folder", Annotations: readOnly,
		Description: "The errors and warnings rein lint reports for a declarations folder, with the file and the rule each comes from."},
		query(func(arguments lintArguments) (any, error) {
			return result(lint.Run(absoluteIn(folder, arguments.Path)))
		}))
}

// absoluteIn is path from folder, the one the agent's session runs in; ""
// leaves a relative path to this process's working folder.
func absoluteIn(folder string, path string) string {
	if filepath.IsAbs(path) || folder == "" {
		return path
	}
	return filepath.Join(folder, path)
}
