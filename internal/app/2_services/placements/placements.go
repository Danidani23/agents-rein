// Package placements is the service that owns what rein placed into an
// agent's home and what it imported: the placements and imports sections of
// the machine state, and the imports folder.
package placements

import (
	"os"
	"path/filepath"

	"github.com/Danidani23/agents-rein/internal/app/2_services/machine"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// Service owns the placements.
type Service struct {
	Home   string
	State  ports.State
	Agents []ports.Agent
}

// ImportsFolder is where imported levels were copied.
func (service Service) ImportsFolder() string {
	return filepath.Join(service.Home, "imports")
}

// List returns the recorded placements and imports.
func (service Service) List() (placements []machine.Placement, imports []machine.Import, err error) {
	state, err := service.State.Load()
	if err != nil {
		return nil, nil, err
	}
	return state.Placements, state.Imports, nil
}

// TargetExists reports whether a placement's target is still there.
func (Service) TargetExists(placed machine.Placement) bool {
	_, err := os.Stat(placed.Target)
	return err == nil
}

// Unplace removes one placement through the agent; removed is false with
// an empty reason when nothing was there, and false with a reason when it
// was not what rein made.
func (service Service) Unplace(placed machine.Placement) (removed bool, reason string, err error) {
	if len(service.Agents) == 0 {
		return false, "no agent adapter to remove it", nil
	}
	return service.Agents[0].Unplace(placed)
}

// Keep rewrites the placements section to the ones given and drops the
// imports, deleting the imports folder.
func (service Service) Keep(kept []machine.Placement) error {
	state, err := service.State.Load()
	if err != nil {
		return err
	}
	state.Placements = kept
	state.Imports = nil
	err = os.RemoveAll(service.ImportsFolder())
	if err != nil {
		return err
	}
	// a config.yaml from before D9 is a leftover
	os.Remove(filepath.Join(service.Home, "config.yaml"))
	return service.State.Save(state)
}
