//go:build windows

package execute

import (
	"os/exec"
	"syscall"
)

// detachedProcess and newProcessGroup are Windows's process creation flags
// for a program without the console of the one that starts it, and outside
// its Ctrl-C (https://learn.microsoft.com/en-us/windows/win32/procthread/process-creation-flags).
const (
	detachedProcess = 0x00000008
	newProcessGroup = 0x00000200
)

// detach starts the program without this process's console and outside
// its Ctrl-C.
func detach(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: detachedProcess | newProcessGroup, HideWindow: true}
}
