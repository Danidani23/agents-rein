// Package version reports which build of reins is running.
package version

import "runtime/debug"

// String is the module version from the build information, or "unknown".
func String() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" {
		return "unknown"
	}
	return info.Main.Version
}
