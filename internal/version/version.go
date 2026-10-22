// Package version reports which build of rein is running.
package version

import "runtime/debug"

// tagged is the release tag the build was made from, set by the release
// build through -ldflags "-X .../internal/version.tagged=v0.1.0"; a build
// without it reports the module's pseudo-version.
var tagged string

// String is the release tag, else the module version from the build
// information, else "unknown".
func String() string {
	if tagged != "" {
		return tagged
	}
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return "unknown"
	}
	return info.Main.Version
}
