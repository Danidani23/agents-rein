package updates

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Danidani23/agents-rein/internal/adapters/fake"
	"github.com/Danidani23/agents-rein/internal/ports"
)

func tarball(t *testing.T, name string, content string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	zipper := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(zipper)
	_ = writer.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg})
	_, _ = writer.Write([]byte(content))
	_ = writer.Close()
	_ = zipper.Close()
	return buffer.Bytes()
}

func zipped(t *testing.T, name string, content string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	file, _ := writer.Create(name)
	_, _ = file.Write([]byte(content))
	_ = writer.Close()
	return buffer.Bytes()
}

func checksumLine(archive []byte, name string) string {
	digest := sha256.Sum256(archive)
	return hex.EncodeToString(digest[:]) + "  " + name + "\n"
}

func TestCheckAndNudge(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	releases := &fake.Releases{Latest_: ports.Release{Version: "v0.2.0"}}
	service := Service{Home: t.TempDir(), Releases: releases, Current: "v0.1.0", Clock: func() time.Time { return now }}
	if _, found := service.Cached(); found {
		t.Fatalf("a cache before any check")
	}
	checked, err := service.Check()
	if err != nil || !checked.Newer || checked.Latest != "v0.2.0" || checked.FromCache || releases.Calls != 1 {
		t.Fatalf("check %+v err %v calls %d", checked, err, releases.Calls)
	}
	checked, err = service.Check()
	if err != nil || !checked.FromCache || releases.Calls != 1 {
		t.Fatalf("the second check within a day asked the host: %+v calls %d", checked, releases.Calls)
	}
	if nudge := service.Nudge(); nudge != "rein v0.2.0 is available, this is v0.1.0; rein update" {
		t.Fatalf("nudge %q", nudge)
	}
	now = now.Add(25 * time.Hour)
	releases.Latest_ = ports.Release{Version: "v0.1.0"}
	checked, _ = service.Check()
	if checked.Newer || releases.Calls != 2 || service.Nudge() != "" {
		t.Fatalf("after a day: %+v calls %d", checked, releases.Calls)
	}
	now = now.Add(25 * time.Hour)
	releases.Fail = errors.New("offline")
	checked, err = service.Check()
	if err != nil || !checked.FromCache || checked.Latest != "v0.1.0" {
		t.Fatalf("offline with a cache: %+v err %v", checked, err)
	}
	offline := Service{Home: t.TempDir(), Releases: releases, Current: "v0.1.0", Clock: func() time.Time { return now }}
	_, err = offline.Check()
	if err == nil || offline.Nudge() != "" {
		t.Fatalf("offline without a cache: err %v", err)
	}
	// a developer's build is older than any release, so it is nudged too, once a day
	releases.Fail = nil
	developer := Service{Home: t.TempDir(), Releases: releases, Current: "v0.0.0-20261022-abc+dirty", Clock: func() time.Time { return now }}
	if nudge := developer.Nudge(); !strings.Contains(nudge, "rein v0.1.0 is available") {
		t.Fatalf("developer nudge %q", nudge)
	}
	// a developer's build after the latest tag is newer than it, so there is nothing to nudge about
	after := Service{Home: t.TempDir(), Releases: releases, Current: "v0.1.1-0.20261022193356-161a1127359e+dirty", Clock: func() time.Time { return now }}
	if nudge := after.Nudge(); nudge != "" {
		t.Fatalf("a build after the tag was nudged: %q", nudge)
	}
}

func TestHowInstalled(t *testing.T) {
	home, _ := os.UserHomeDir()
	t.Setenv("GOPATH", "/go")
	t.Setenv("GOBIN", "")
	for executable, expected := range map[string]string{
		"/opt/homebrew/bin/rein":                         InstallBrew,
		"/usr/local/Cellar/rein/0.1.0/bin/rein":          InstallBrew,
		"C:/Users/dani/scoop/apps/rein/current/rein.exe": InstallScoop,
		"/go/bin/rein":                                   InstallGo,
		filepath.Join(home, ".local", "bin", "rein"):     InstallScript,
		"/usr/bin/rein":                                  InstallUnknown,
	} {
		install := Service{Executable: executable}.HowInstalled()
		if install.Kind != expected || (expected == InstallScript) != install.Self || (expected == InstallScript || expected == InstallUnknown) != (install.Command == "") {
			t.Fatalf("%s: %+v", executable, install)
		}
	}
}

func TestSelfUpdate(t *testing.T) {
	folder := t.TempDir()
	executable := filepath.Join(folder, "rein")
	_ = os.WriteFile(executable, []byte("old"), 0o755)
	archive := tarball(t, "rein", "new binary")
	releases := &fake.Releases{Latest_: ports.Release{Version: "v0.2.0", Assets: map[string]string{"rein_0.2.0_linux_arm64.tar.gz": "asset", "rein_0.2.0_checksums.txt": "sums"}}, Files: map[string][]byte{"asset": archive, "sums": []byte(checksumLine(archive, "rein_0.2.0_linux_arm64.tar.gz"))}}
	service := Service{Home: t.TempDir(), Releases: releases, Current: "v0.1.0", Executable: executable, OS: "linux", Arch: "arm64"}
	if !service.Writable() {
		t.Fatalf("a temp folder is not writable")
	}
	replaced, err := service.SelfUpdate(releases.Latest_)
	content, _ := os.ReadFile(executable)
	info, _ := os.Stat(executable)
	// Windows keeps no Unix permission bits
	if err != nil || replaced != executable || string(content) != "new binary" || (runtime.GOOS != "windows" && info.Mode()&0o100 == 0) {
		t.Fatalf("replaced %q err %v content %q mode %v", replaced, err, content, info.Mode())
	}
	releases.Files["sums"] = []byte("0000 rein_0.2.0_linux_arm64.tar.gz\n")
	_, err = service.SelfUpdate(releases.Latest_)
	if err == nil || !strings.Contains(err.Error(), "does not match its checksum") {
		t.Fatalf("a wrong checksum installed: %v", err)
	}
	releases.Files["sums"] = []byte("")
	_, err = service.SelfUpdate(releases.Latest_)
	if err == nil || !strings.Contains(err.Error(), "names no rein_0.2.0_linux_arm64.tar.gz") {
		t.Fatalf("an absent checksum installed: %v", err)
	}
	_, err = Service{Releases: releases, Executable: executable, OS: "plan9", Arch: "mips"}.SelfUpdate(releases.Latest_)
	if err == nil || !strings.Contains(err.Error(), "no build for plan9/mips") {
		t.Fatalf("a missing build: %v", err)
	}
	windows := zipped(t, "rein.exe", "exe")
	releases.Latest_.Assets["rein_0.2.0_windows_amd64.zip"] = "winasset"
	releases.Files["winasset"] = windows
	releases.Files["sums"] = []byte(checksumLine(windows, "rein_0.2.0_windows_amd64.zip"))
	_, err = Service{Releases: releases, Executable: executable, OS: "windows", Arch: "amd64"}.SelfUpdate(releases.Latest_)
	content, _ = os.ReadFile(executable)
	if err != nil || string(content) != "exe" {
		t.Fatalf("windows: err %v content %q", err, content)
	}
	if AssetName("v1.2.3", "darwin", "arm64") != "rein_1.2.3_darwin_arm64.tar.gz" || AssetName("v1.2.3", "windows", "amd64") != "rein_1.2.3_windows_amd64.zip" {
		t.Fatalf("asset names")
	}
}
