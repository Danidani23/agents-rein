package execute

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// lookPathIn finds name on the PATH of environment, the way exec.LookPath
// finds it on this process's: a name holding a separator is taken as it
// is; a folder that is not absolute is skipped, as exec refuses a program
// found through one; on Windows the extensions of PATHEXT are tried.
func lookPathIn(name string, environment []string) (string, error) {
	if strings.ContainsAny(name, `/\`) {
		return name, nil
	}
	extensions := []string{""}
	if runtime.GOOS == "windows" {
		extensions = windowsExtensions(name, Runner{Environment: environment}.Getenv("PATHEXT"))
	}
	for _, folder := range filepath.SplitList(Runner{Environment: environment}.Getenv("PATH")) {
		if !filepath.IsAbs(folder) {
			continue
		}
		for _, extension := range extensions {
			candidate := filepath.Join(folder, name+extension)
			if isProgram(candidate) {
				return candidate, nil
			}
		}
	}
	return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
}

// windowsExtensions are the endings Windows tries after name: none when
// name has one, then those of PATHEXT, or Windows's own list without it.
func windowsExtensions(name string, pathExtensions string) []string {
	if pathExtensions == "" {
		pathExtensions = ".com;.exe;.bat;.cmd"
	}
	var extensions []string
	if filepath.Ext(name) != "" {
		extensions = append(extensions, "")
	}
	for _, extension := range strings.Split(strings.ToLower(pathExtensions), ";") {
		if strings.HasPrefix(extension, ".") {
			extensions = append(extensions, extension)
		}
	}
	return extensions
}

// isProgram says whether path is a file this system would run.
func isProgram(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return runtime.GOOS == "windows" || info.Mode()&0o111 != 0
}
