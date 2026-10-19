package declaration

import (
	"fmt"
	"sort"
	"strings"
)

// ToolServer is an MCP server declaration, mcpservers/<id>.yaml. Its shape
// mirrors the .mcp.json entry that Claude Code and other agents read, so the
// compiler copies it; secrets are never in it, only ${REIN_...} references
// to variables of the developer's shell, each described under variables.
type ToolServer struct {
	Selectors `yaml:",inline"`
	Transport string            `yaml:"transport"`
	Command   string            `yaml:"command,omitempty"`
	Args      []string          `yaml:"args,omitempty"`
	URL       string            `yaml:"url,omitempty"`
	Env       map[string]string `yaml:"env,omitempty"`
	Headers   map[string]string `yaml:"headers,omitempty"`
	Variables map[string]string `yaml:"variables,omitempty"`
}

// Transports a tool server may use.
var Transports = []string{"stdio", "http", "sse"}

// ParseToolServer reads one tool server declaration; file is its name
// without the extension.
func ParseToolServer(file string, content []byte) (server ToolServer, problems []Problem) {
	problems = decodeStrict(content, &server, file+".yaml")
	if problems != nil {
		return ToolServer{}, problems
	}
	problems = append(problems, server.Selectors.check()...)
	switch server.Transport {
	case "":
		problems = append(problems, Problem{Severity: SeverityError, Message: "transport is required: " + strings.Join(Transports, ", ")})
	case "stdio":
		if server.Command == "" {
			problems = append(problems, Problem{Severity: SeverityError, Message: "a stdio server needs a command"})
		}
		if server.URL != "" {
			problems = append(problems, Problem{Severity: SeverityError, Message: "a stdio server has no url"})
		}
	case "http", "sse":
		if server.URL == "" {
			problems = append(problems, Problem{Severity: SeverityError, Message: fmt.Sprintf("an %s server needs a url", server.Transport)})
		}
		if server.Command != "" {
			problems = append(problems, Problem{Severity: SeverityError, Message: fmt.Sprintf("an %s server has no command", server.Transport)})
		}
	default:
		problems = append(problems, Problem{Severity: SeverityError, Message: fmt.Sprintf("transport %q is not one of %s", server.Transport, strings.Join(Transports, ", "))})
	}
	referenced := map[string]bool{}
	problems = append(problems, checkValues("env", server.Env, server.Variables, referenced)...)
	problems = append(problems, checkValues("header", server.Headers, server.Variables, referenced)...)
	var unused []string
	for name, description := range server.Variables {
		if description == "" {
			problems = append(problems, Problem{Severity: SeverityError, Message: fmt.Sprintf("variable %s needs a description: what it is and where to get it", name)})
		}
		if !referenced[name] {
			unused = append(unused, name)
		}
	}
	sort.Strings(unused)
	for _, name := range unused {
		problems = append(problems, Problem{Severity: SeverityWarning, Message: fmt.Sprintf("variable %s is described but nothing references it", name)})
	}
	return server, problems
}
