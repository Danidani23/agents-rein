package main

import (
	"fmt"
	"strconv"
	"strings"
)

// version is a release tag, vMAJOR.MINOR.PATCH.
type version struct {
	major, minor, patch int
}

// firstRelease is the version of a repository's first release.
var firstRelease = version{0, 1, 0}

func (release version) String() string {
	return fmt.Sprintf("v%d.%d.%d", release.major, release.minor, release.patch)
}

// plain is the version without the v, as the changelog heads a section.
func (release version) plain() string {
	return strings.TrimPrefix(release.String(), "v")
}

// parseVersion reads a tag; anything but vMAJOR.MINOR.PATCH is not a release.
func parseVersion(tag string) (version, bool) {
	segments := strings.Split(strings.TrimPrefix(strings.TrimSpace(tag), "v"), ".")
	if len(segments) != 3 || !strings.HasPrefix(strings.TrimSpace(tag), "v") {
		return version{}, false
	}
	var parts [3]int
	for index, segment := range segments {
		value, err := strconv.Atoi(segment)
		if err != nil || value < 0 {
			return version{}, false
		}
		parts[index] = value
	}
	return version{parts[0], parts[1], parts[2]}, true
}

// latestRelease is the newest release among tags; found is false when none
// is a release.
func latestRelease(tags []string) (latest version, found bool) {
	for _, tag := range tags {
		release, ok := parseVersion(tag)
		if !ok {
			continue
		}
		if !found || latest.less(release) {
			latest, found = release, true
		}
	}
	return latest, found
}

func (release version) less(other version) bool {
	if release.major != other.major {
		return release.major < other.major
	}
	if release.minor != other.minor {
		return release.minor < other.minor
	}
	return release.patch < other.patch
}

// tally is what the commits since the last release amount to.
type tally struct {
	breaking, feat, fix, perf, other int
}

// releasable says whether the commits warrant a release: a change a user
// of rein sees. Docs, tests, chores and refactors alone release nothing.
func (counted tally) releasable() bool {
	return counted.breaking+counted.feat+counted.fix+counted.perf > 0
}

func (counted tally) String() string {
	return fmt.Sprintf("%d breaking, %d feat, %d fix, %d perf, %d other", counted.breaking, counted.feat, counted.fix, counted.perf, counted.other)
}

// nextVersion applies the tally to the last release; callers check
// releasable first.
func nextVersion(last version, counted tally) version {
	switch {
	case counted.breaking > 0:
		return version{last.major + 1, 0, 0}
	case counted.feat > 0:
		return version{last.major, last.minor + 1, 0}
	default:
		return version{last.major, last.minor, last.patch + 1}
	}
}
