// Command rein is Agents Rein: a compiler and a linter for agent settings held
// in an org repository. It never runs as a service, never pushes into a
// repository and never writes a vendor's policy path. This file is the
// composition root: it wires the adapters of this machine into the services,
// the services into the local orchestrator, and hands that to the gateway.
package main

import (
	"os"

	"github.com/Danidani23/agents-rein/internal/adapters/claudecode"
	"github.com/Danidani23/agents-rein/internal/adapters/git"
	"github.com/Danidani23/agents-rein/internal/adapters/github"
	"github.com/Danidani23/agents-rein/internal/adapters/localstate"
	"github.com/Danidani23/agents-rein/internal/app/0_gateway/cli"
	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
	"github.com/Danidani23/agents-rein/internal/ports"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, wire))
}

// wire builds the local orchestrator over this machine's adapters.
func wire() (orchestration.Orchestrator, error) {
	home, err := localstate.Home()
	if err != nil {
		return orchestration.Orchestrator{}, err
	}
	services := orchestration.NewServices(home, github.Host{}, git.Repository{}, localstate.Store{Home: home}, []ports.Agent{claudecode.Agent{}})
	return orchestration.NewLocal(services), nil
}
