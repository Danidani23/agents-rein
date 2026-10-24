// Package updates is the service that knows which rein is running, whether
// a newer one is published, how this one was installed and how it is
// updated: by the package manager that installed it, or by rein itself for
// a script install (P2.8).
package updates

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Danidani23/agents-rein/internal/app/2_services/semver"
	"github.com/Danidani23/agents-rein/internal/ports"
)

// Service checks and applies updates. Current is the running version,
// Executable where it is, Clock the time (D20).
type Service struct {
	Home       string
	Releases   ports.Releases
	Current    string
	Executable string
	Clock      func() time.Time
	// OS and Arch name the build to fetch; empty means this machine's.
	OS   string
	Arch string
}

// checkInterval is how long a check result holds before the host is asked
// again.
const checkInterval = 24 * time.Hour

// cacheFile is where the last check is kept under the home.
const cacheFile = "update-check.yaml"

// Check is what the last look at the releases found.
type Check struct {
	Current   string    `yaml:"current"`
	Latest    string    `yaml:"latest"`
	CheckedAt time.Time `yaml:"checked_at"`
	// Newer says whether Latest is a release newer than Current.
	Newer bool `yaml:"newer"`
	// FromCache says the result was not fetched now.
	FromCache bool `yaml:"-"`
}

// Install says how rein got onto this machine and how it is updated.
type Install struct {
	// Kind is one of the install kinds below.
	Kind string
	// Command is what updates a managed install; "" otherwise.
	Command string
	// Advice says how to update an install no one command updates; "" otherwise.
	Advice string
	// Self says rein replaces the binary itself: a script install.
	Self bool
}

// The install kinds.
const (
	InstallBrew    = "brew"
	InstallScoop   = "scoop"
	InstallWinget  = "winget"
	InstallGo      = "go"
	InstallNix     = "Nix"
	InstallPackage = "a Linux package"
	InstallScript  = "script"
	InstallUnknown = "unknown"
)

func (service Service) now() time.Time {
	if service.Clock == nil {
		return time.Now()
	}
	return service.Clock()
}

func (service Service) cachePath() string {
	return filepath.Join(service.Home, cacheFile)
}

// Cached is the last check, if one was made.
func (service Service) Cached() (Check, bool) {
	content, err := os.ReadFile(service.cachePath())
	if err != nil {
		return Check{}, false
	}
	var cached Check
	err = yaml.Unmarshal(content, &cached)
	if err != nil {
		return Check{}, false
	}
	cached.FromCache = true
	cached.Newer = semver.Compare(cached.Latest, service.Current) > 0
	return cached, true
}

// CheckNow asks the host for the latest release and remembers the answer.
func (service Service) CheckNow() (Check, error) {
	latest, err := service.Releases.Latest()
	if err != nil {
		return Check{}, err
	}
	checked := Check{Current: service.Current, Latest: latest.Version, CheckedAt: service.now(), Newer: semver.Compare(latest.Version, service.Current) > 0}
	encoded, err := yaml.Marshal(checked)
	if err != nil {
		return Check{}, err
	}
	err = os.MkdirAll(service.Home, 0o755)
	if err != nil {
		return Check{}, err
	}
	err = os.WriteFile(service.cachePath(), encoded, 0o644)
	if err != nil {
		return Check{}, err
	}
	return checked, nil
}

// Check is CheckNow at most once a day; in between, the cached answer.
func (service Service) Check() (Check, error) {
	cached, found := service.Cached()
	if found && service.now().Sub(cached.CheckedAt) < checkInterval {
		return cached, nil
	}
	checked, err := service.CheckNow()
	if err != nil && found {
		return cached, nil
	}
	return checked, err
}

// Nudge is the one line a command may print when a newer rein is published,
// never more than once a day; "" otherwise. A failure to check stays quiet.
func (service Service) Nudge() string {
	checked, err := service.Check()
	if err != nil || !checked.Newer {
		return ""
	}
	return fmt.Sprintf("rein %s is available, this is %s; rein update", checked.Latest, checked.Current)
}

// HowInstalled reads the install from where the binary lives.
func (service Service) HowInstalled() Install {
	executable := filepath.ToSlash(service.Executable)
	home, _ := os.UserHomeDir()
	localApplicationData := os.Getenv("LOCALAPPDATA")
	switch {
	case strings.Contains(executable, "/Cellar/") || strings.Contains(executable, "/homebrew/") || strings.Contains(executable, "/linuxbrew/"):
		return Install{Kind: InstallBrew, Command: "brew upgrade --cask rein"}
	case strings.Contains(strings.ToLower(executable), "/scoop/"):
		return Install{Kind: InstallScoop, Command: "scoop update rein"}
	// winget starts rein through a link in its Links folder, which os.Executable may report
	case strings.Contains(strings.ToLower(executable), "/winget/packages/") || strings.Contains(strings.ToLower(executable), "/winget/links/"):
		return Install{Kind: InstallWinget, Command: "winget upgrade Danidani23.rein"}
	case strings.HasPrefix(executable, "/nix/store/"):
		return Install{Kind: InstallNix, Advice: "update it the way it was installed: nix profile upgrade rein, or the NixOS or home-manager configuration that lists it"}
	// the .deb, .rpm and .apk of a release and the AUR's rein-bin all put rein here
	case executable == "/usr/bin/rein":
		return Install{Kind: InstallPackage, Advice: "install the new release's .deb, .rpm or .apk from https://github.com/Danidani23/agents-rein/releases/latest, or, for the AUR's rein-bin, upgrade with your AUR helper"}
	case strings.HasPrefix(executable, filepath.ToSlash(goBinFolder())+"/"):
		return Install{Kind: InstallGo, Command: "go install github.com/Danidani23/agents-rein/cmd/rein@latest"}
	case home != "" && strings.HasPrefix(executable, filepath.ToSlash(filepath.Join(home, ".local", "bin"))+"/"):
		return Install{Kind: InstallScript, Self: true}
	case localApplicationData != "" && strings.HasPrefix(executable, filepath.ToSlash(filepath.Join(localApplicationData, "Programs", "rein"))+"/"):
		return Install{Kind: InstallScript, Self: true}
	}
	return Install{Kind: InstallUnknown}
}

// goBinFolder is where go install puts binaries.
func goBinFolder() string {
	if goBin := os.Getenv("GOBIN"); goBin != "" {
		return goBin
	}
	if goPath := os.Getenv("GOPATH"); goPath != "" {
		return filepath.Join(goPath, "bin")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "go", "bin")
}

// AssetName is the archive of a release for an operating system and an
// architecture, as the release workflow names them.
func AssetName(releaseVersion string, operatingSystem string, architecture string) string {
	extension := "tar.gz"
	if operatingSystem == "windows" {
		extension = "zip"
	}
	return fmt.Sprintf("rein_%s_%s_%s.%s", strings.TrimPrefix(releaseVersion, "v"), operatingSystem, architecture, extension)
}

// SelfUpdate replaces the running binary with the release's build for this
// machine, after checking the archive against the release's checksum file:
// download, verify, extract, write beside the binary, rename over it. Windows
// refuses to replace a running binary but lets it be renamed, so there the old
// one moves aside to .old first, and the next update replaces what it left.
func (service Service) SelfUpdate(release ports.Release) (string, error) {
	operatingSystem, architecture := service.OS, service.Arch
	if operatingSystem == "" {
		operatingSystem = runtime.GOOS
	}
	if architecture == "" {
		architecture = runtime.GOARCH
	}
	assetName := AssetName(release.Version, operatingSystem, architecture)
	assetURL, found := release.Assets[assetName]
	if !found {
		return "", fmt.Errorf("release %s has no build for %s/%s; its files are %s", release.Version, operatingSystem, architecture, strings.Join(sortedNames(release.Assets), ", "))
	}
	checksumsURL, found := release.Assets[fmt.Sprintf("rein_%s_checksums.txt", strings.TrimPrefix(release.Version, "v"))]
	if !found {
		return "", fmt.Errorf("release %s has no checksum file; not installing an unverified build", release.Version)
	}
	archive, err := service.Releases.Download(assetURL)
	if err != nil {
		return "", err
	}
	checksums, err := service.Releases.Download(checksumsURL)
	if err != nil {
		return "", err
	}
	err = verifyChecksum(archive, assetName, string(checksums))
	if err != nil {
		return "", err
	}
	binary, err := extractBinary(archive, operatingSystem)
	if err != nil {
		return "", err
	}
	temporary := service.Executable + ".new"
	err = os.WriteFile(temporary, binary, 0o755)
	if err != nil {
		return "", err
	}
	setAside := service.Executable + ".old"
	if runtime.GOOS == "windows" {
		err = os.Rename(service.Executable, setAside)
		if err != nil {
			os.Remove(temporary)
			return "", err
		}
	}
	err = os.Rename(temporary, service.Executable)
	if err != nil {
		os.Remove(temporary)
		if runtime.GOOS == "windows" {
			os.Rename(setAside, service.Executable)
		}
		return "", err
	}
	return service.Executable, nil
}

// verifyChecksum finds the archive's line in the checksum file and compares.
func verifyChecksum(archive []byte, assetName string, checksums string) error {
	digest := sha256.Sum256(archive)
	actual := hex.EncodeToString(digest[:])
	for _, line := range strings.Split(checksums, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == assetName {
			if fields[0] != actual {
				return fmt.Errorf("%s does not match its checksum: expected %s, got %s; not installing it", assetName, fields[0], actual)
			}
			return nil
		}
	}
	return fmt.Errorf("the checksum file names no %s; not installing an unverified build", assetName)
}

// extractBinary takes the rein binary out of a tar.gz or a zip archive.
func extractBinary(archive []byte, operatingSystem string) ([]byte, error) {
	wanted := "rein"
	if operatingSystem == "windows" {
		wanted = "rein.exe"
		reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, fmt.Errorf("the archive is not a zip: %w", err)
		}
		for _, file := range reader.File {
			if filepath.Base(file.Name) == wanted {
				opened, err := file.Open()
				if err != nil {
					return nil, err
				}
				defer opened.Close()
				return io.ReadAll(opened)
			}
		}
		return nil, fmt.Errorf("the archive holds no %s", wanted)
	}
	unzipped, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("the archive is not a tar.gz: %w", err)
	}
	reader := tar.NewReader(unzipped)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("the archive holds no %s", wanted)
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag == tar.TypeReg && filepath.Base(header.Name) == wanted {
			return io.ReadAll(reader)
		}
	}
}

func sortedNames(assets map[string]string) []string {
	var names []string
	for name := range assets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// writable says whether the binary's folder can be written, which a
// self-update needs.
func (service Service) Writable() bool {
	probe := service.Executable + ".probe"
	err := os.WriteFile(probe, nil, 0o600)
	if err != nil {
		return false
	}
	os.Remove(probe)
	return true
}
