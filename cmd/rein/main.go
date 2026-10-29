// Command rein is Agents Rein: a compiler and a linter for agent settings held
// in an org repository. It never pushes into a repository and never writes a
// vendor's policy path. Its commands run in the background process of rein's
// home, which the first one starts (D52, D55). This file is the composition
// root: it wires the adapters of this machine, for the caller a command runs
// for, into the services, the services into the local orchestrator, and
// hands that to the gateway.
package main

import (
	"os"

	"github.com/Danidani23/agents-rein/internal/adapters/claudecode"
	"github.com/Danidani23/agents-rein/internal/adapters/execute"
	"github.com/Danidani23/agents-rein/internal/adapters/git"
	"github.com/Danidani23/agents-rein/internal/adapters/github"
	"github.com/Danidani23/agents-rein/internal/adapters/keychain"
	"github.com/Danidani23/agents-rein/internal/adapters/localstate"
	"github.com/Danidani23/agents-rein/internal/adapters/messagesapi"
	"github.com/Danidani23/agents-rein/internal/adapters/release"
	"github.com/Danidani23/agents-rein/internal/app/0_gateway/cli"
	"github.com/Danidani23/agents-rein/internal/app/1_orchestration"
	"github.com/Danidani23/agents-rein/internal/app/2_services/declaration"
	"github.com/Danidani23/agents-rein/internal/ports"
	"github.com/Danidani23/agents-rein/internal/version"
)

// The addresses of GitHub; REIN_GITHUB_WEB and REIN_GITHUB_API point a test
// or a GitHub Enterprise elsewhere.
const (
	gitHubWeb = "https://github.com"
	gitHubAPI = "https://api.github.com"
)

func main() {
	os.Exit(runRein(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// wire builds the local orchestrator over this machine's adapters, for
// this process as the caller.
func wire() (orchestration.Orchestrator, error) {
	executable, _ := os.Executable()
	return wireFor(execute.Runner{}, executable)()
}

// wireFor builds it for a caller: its runner, which carries its
// environment and runs its programs, and its binary.
func wireFor(runner execute.Runner, executable string) cli.Orchestrate {
	return func() (orchestration.Orchestrator, error) {
		home, err := localstate.Home()
		if err != nil {
			return orchestration.Orchestrator{}, err
		}
		if executable == "" {
			executable, _ = os.Executable()
		}
		valueOr := func(variable string, fallback string) string {
			value := runner.Getenv(variable)
			if value == "" {
				return fallback
			}
			return value
		}
		store := localstate.Store{Home: home}
		host := github.NewClient(github.ClientID, valueOr("REIN_GITHUB_WEB", gitHubWeb), valueOr("REIN_GITHUB_API", gitHubAPI))
		var tokens ports.Keychain = keychain.System{Runner: runner}
		if file := runner.Getenv("REIN_KEYCHAIN_FILE"); file != "" {
			// a file in place of the keychain: for tests and machines without one, chosen on purpose
			tokens = keychain.FileKeychain{Path: file}
		}
		// every command of this process takes turns at the machine lock through one queue (D56)
		services := orchestration.NewServices(home, host, git.Repository{Runner: runner}, store, []ports.Agent{claudecode.Agent{Runner: runner}}, claudecode.Judge{Runner: runner}, localstate.QueueOf(home), tokens, release.NewClient(valueOr("REIN_GITHUB_API", gitHubAPI)), version.String(), executable)
		services.Credentials.Environment = runner.LookupEnv
		services.Updates.Environment = runner.LookupEnv
		services.WorkingCopy.Environment = runner.LookupEnv
		// an organisation that declares its distiller is asked through the Messages API (D42)
		services.Judge.Provider = func(provider declaration.Provider) ports.Judge {
			return messagesapi.Judge{Provider: provider, Lookup: services.Credentials.Key}
		}
		return orchestration.New(services), nil
	}
}
