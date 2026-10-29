//go:build !windows

package execute

import (
	"os/exec"
	"syscall"
)

// detach puts the program in a session of its own, so the terminal's
// Ctrl-C and its closing do not reach it.
func detach(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
