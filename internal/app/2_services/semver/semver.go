// Package semver reads and orders versions of the form
// vMAJOR.MINOR.PATCH[-PRERELEASE][+BUILD], for the update check and the
// organisation's minimum. Go names a build after a tag with the next patch
// and a pre-release, v0.1.1-0.20261022193356-161a1127359e, so such a build
// orders after the tag and before the next release.
package semver

import (
	"strings"
)

// parsed is a version taken apart.
type parsed struct {
	numbers    [3]int
	prerelease string
}

// IsRelease says whether a text is a release version, vMAJOR.MINOR.PATCH
// with or without the v and nothing after it: not a pre-release, not a
// build after a tag, not a dirty build, not "unknown".
func IsRelease(text string) bool {
	version, ok := parse(text)
	return ok && version.prerelease == "" && !strings.Contains(text, "+")
}

// Compare orders two versions: negative when left is older than right, zero
// when equal, positive when newer. The numbers decide first; at equal
// numbers a pre-release is older than the release, and two pre-releases
// order by their text, which puts an earlier build before a later one.
// Build metadata after a + is ignored. A text that is no version at all
// counts as older than any version, and two such texts as equal.
func Compare(left string, right string) int {
	leftVersion, leftOK := parse(left)
	rightVersion, rightOK := parse(right)
	switch {
	case !leftOK && !rightOK:
		return 0
	case !leftOK:
		return -1
	case !rightOK:
		return 1
	}
	for index := 0; index < 3; index++ {
		if leftVersion.numbers[index] != rightVersion.numbers[index] {
			return leftVersion.numbers[index] - rightVersion.numbers[index]
		}
	}
	switch {
	case leftVersion.prerelease == rightVersion.prerelease:
		return 0
	case leftVersion.prerelease == "":
		return 1
	case rightVersion.prerelease == "":
		return -1
	}
	return strings.Compare(leftVersion.prerelease, rightVersion.prerelease)
}

// parse reads vMAJOR.MINOR.PATCH, a pre-release after a hyphen, build
// metadata after a plus.
func parse(text string) (parsed, bool) {
	var version parsed
	core := strings.TrimPrefix(strings.TrimSpace(text), "v")
	if plus := strings.Index(core, "+"); plus >= 0 {
		core = core[:plus]
	}
	if hyphen := strings.Index(core, "-"); hyphen >= 0 {
		version.prerelease = core[hyphen+1:]
		core = core[:hyphen]
	}
	segments := strings.Split(core, ".")
	if len(segments) != 3 {
		return parsed{}, false
	}
	for index, segment := range segments {
		if segment == "" {
			return parsed{}, false
		}
		value := 0
		for _, digit := range segment {
			if digit < '0' || digit > '9' {
				return parsed{}, false
			}
			value = value*10 + int(digit-'0')
		}
		version.numbers[index] = value
	}
	return version, true
}
