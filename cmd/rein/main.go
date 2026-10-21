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
	"github.com/Danidani23/agents-rein/internal/adapters/keychain"
	"github.com/Danidani23/agents-rein/internal/adapters/localstate"
	"github.com/Danidani23/agents-rein/internal/app/0_gateway/cli"
	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// The addresses of GitHub; REIN_GITHUB_WEB and REIN_GITHUB_API point a test
// or a GitHub Enterprise elsewhere.
const (
	gitHubWeb = "https://github.com"
	gitHubAPI = "https://api.github.com"
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
	store := localstate.Store{Home: home}
	host := github.NewClient(github.ClientID, valueOr("REIN_GITHUB_WEB", gitHubWeb), valueOr("REIN_GITHUB_API", gitHubAPI))
	var tokens ports.Keychain = keychain.System{}
	if file := os.Getenv("REIN_KEYCHAIN_FILE"); file != "" {
		// a file in place of the keychain: for tests and machines without one, chosen on purpose
		tokens = keychain.FileKeychain{Path: file}
	}
	services := orchestration.NewServices(home, host, git.Repository{}, store, []ports.Agent{claudecode.Agent{}}, claudecode.Judge{}, store, tokens)
	return orchestration.New(services), nil
}

func valueOr(variable string, fallback string) string {
	value := os.Getenv(variable)
	if value == "" {
		return fallback
	}
	return value
}
