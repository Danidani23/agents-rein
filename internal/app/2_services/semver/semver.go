// Package semver reads and orders release versions of the form
// vMAJOR.MINOR.PATCH, for the update check and the organisation's minimum.
package semver

import "strings"

// IsRelease says whether a text is a release version, vMAJOR.MINOR.PATCH
// with or without the v, rather than a pseudo-version, a dirty build or
// "unknown".
func IsRelease(text string) bool {
	_, ok := parse(text)
	return ok
}

// Compare orders two versions: negative when left is older than right, zero
// when equal, positive when newer. A text that is no release counts as
// older than any release, and two such texts as equal.
func Compare(left string, right string) int {
	leftParts, leftOK := parse(left)
	rightParts, rightOK := parse(right)
	switch {
	case !leftOK && !rightOK:
		return 0
	case !leftOK:
		return -1
	case !rightOK:
		return 1
	}
	for index := 0; index < 3; index++ {
		if leftParts[index] != rightParts[index] {
			return leftParts[index] - rightParts[index]
		}
	}
	return 0
}

// parse reads vMAJOR.MINOR.PATCH.
func parse(text string) ([3]int, bool) {
	var parts [3]int
	segments := strings.Split(strings.TrimPrefix(strings.TrimSpace(text), "v"), ".")
	if len(segments) != 3 {
		return parts, false
	}
	for index, segment := range segments {
		if segment == "" {
			return parts, false
		}
		value := 0
		for _, digit := range segment {
			if digit < '0' || digit > '9' {
				return parts, false
			}
			value = value*10 + int(digit-'0')
		}
		parts[index] = value
	}
	return parts, true
}
