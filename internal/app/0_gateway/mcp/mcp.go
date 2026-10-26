// Package mcp is rein's second gateway: the queries the CLI prints, offered
// to an agent as tools of the Model Context Protocol over standard input and
// output. It holds the same concrete Orchestrator as the CLI and nothing else
// of the inside. Every tool of this version reads: it takes no machine lock,
// opens no pull request and writes no file, and says so with readOnlyHint, so
// the agent's own permission rules decide what runs unasked. The protocol is
// spoken by the official SDK (docs/decisions/2026-10-23_mcp-gateway-on-the-official-sdk.md).
package mcp

import (
	"context"
	"errors"
	"io"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/refusal"
)

// ServerName is how the agent addresses the server: its tools are
// mcp__rein__<tool>.
const ServerName = "rein"

// Instructions tell the agent what the tools are for, before it lists them.
const Instructions = "rein keeps the settings of AI coding agents as declarations in one repository per organisation and writes them into each project. These tools read what rein knows: this machine, what a project resolves and why, what a declaration holds, which proposals wait, and what a declarations folder lints to. None of them changes anything; rein sync, distill, propose and reset are the developer's commands."

// Serve speaks MCP over in and out until in is closed, which is how the
// agent ends the session. version is what rein version prints.
func Serve(ctx context.Context, in io.Reader, out io.Writer, orchestrator orchestration.Orchestrator, version string) error {
	return NewServer(orchestrator, version).Run(ctx, &mcp.IOTransport{Reader: io.NopCloser(in), Writer: nopCloser{out}})
}

// NewServer builds the server with every tool; a test connects it over an
// in-memory transport.
func NewServer(orchestrator orchestration.Orchestrator, version string) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: ServerName, Title: "Agents Rein", Version: version}, &mcp.ServerOptions{Instructions: Instructions})
	addTools(server, orchestrator)
	return server
}

// nopCloser lets the SDK close its writer without closing the stream it
// was handed.
type nopCloser struct{ io.Writer }

func (nopCloser) Close() error { return nil }

// query adapts an orchestrator call to a tool handler. The answer goes back
// as structured content, which the SDK also renders as JSON text. A refusal
// is a tool error the agent can act on; any other error is a protocol error.
func query[In any](call func(In) (any, error)) mcp.ToolHandlerFor[In, any] {
	return func(_ context.Context, _ *mcp.CallToolRequest, input In) (*mcp.CallToolResult, any, error) {
		answer, err := call(input)
		if err != nil {
			if errors.Is(err, refusal.Refusal{}) {
				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}, IsError: true}, nil, nil
			}
			return nil, nil, err
		}
		return nil, answer, nil
	}
}

// result pairs a call's two return values for query.
func result(value any, err error) (any, error) {
	return value, err
}

// requireLogin is the identity gate the CLI puts in front of a command that
// acts for a user, for the tools whose command has it.
func requireLogin(orchestrator orchestration.Orchestrator) error {
	logins, err := orchestrator.Credentials.Logins()
	if err != nil {
		return err
	}
	if len(logins) == 0 {
		return refusal.New("not logged in; the developer runs rein login, which opens the GitHub device flow")
	}
	return nil
}
