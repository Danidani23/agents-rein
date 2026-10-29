package cli

import (
	"github.com/spf13/cobra"

	mcpgateway "github.com/Danidani23/agents-rein/internal/app/0_gateway/mcp"
	"github.com/Danidani23/agents-rein/internal/version"
)

func newMCPCommand(environment *commandEnvironment) *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Serve rein's queries to an agent as MCP tools over stdin and stdout",
		Long: "mcp speaks the Model Context Protocol over standard input and output for the agent that\n" +
			"started it: rein sync writes the entry into .mcp.json, so an agent that opens the clone finds\n" +
			"the server rein. The tools are the read-only queries, status, resolve, declaration_list,\n" +
			"declaration_describe, organization_describe, proposals_pending and lint, each marked\n" +
			"read-only, so the agent's own permission rules decide which run unasked. It runs until the\n" +
			"agent closes the input and prints nothing a person reads.",
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, arguments []string) error {
			orchestrator, err := environment.orchestrate()
			if err != nil {
				return err
			}
			return mcpgateway.Serve(command.Context(), environment.in, environment.out, orchestrator, version.String(), environment.folder)
		},
	}
}
