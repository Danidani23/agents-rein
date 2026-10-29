package execute

import (
	"os"
	"os/exec"
)

// StartDetached starts name with arguments in directory, apart from this
// process, so it goes on after this process ends: the background process,
// which the first command that needs it starts. Its output is appended to
// log; it gets no input and no time limit, and nothing waits for it.
func (runner Runner) StartDetached(directory string, log string, name string, arguments ...string) error {
	path, err := runner.lookPath(name)
	if err != nil {
		return err
	}
	output, err := os.OpenFile(log, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer output.Close()
	command := exec.Command(path, arguments...)
	command.Dir = directory
	command.Env = runner.environment(Options{})
	command.Stdout = output
	command.Stderr = output
	detach(command)
	err = command.Start()
	if err != nil {
		return err
	}
	return command.Process.Release()
}
