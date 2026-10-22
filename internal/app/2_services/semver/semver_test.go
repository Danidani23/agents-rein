package semver

import "testing"

func TestCompareAndIsRelease(t *testing.T) {
	for _, testCase := range []struct {
		left, right string
		expected    int
	}{
		{"v0.1.0", "v0.1.0", 0}, {"v0.1.0", "v0.2.0", -1}, {"v1.0.0", "v0.9.9", 1}, {"0.1.0", "v0.1.0", 0},
		{"v0.0.0-20261022-abc", "v0.1.0", -1}, {"unknown", "v0.1.0", -1}, {"v0.1.0", "unknown", 1}, {"unknown", "v0.0.0-x", -1},
		{"v0.10.0", "v0.9.0", 1}, {"v1.2.3+dirty", "v1.2.3", 0},
		// a build after a tag carries the next patch with a pre-release: newer than the tag, older than the next release
		{"v0.1.1-0.20261022193356-161a1127359e", "v0.1.0", 1}, {"v0.1.1-0.20261022193356-161a1127359e", "v0.1.1", -1},
		{"v0.1.1-0.20261022193356-161a1127359e+dirty", "v0.1.0", 1}, {"v0.1.1-0.20261022193356-161a1127359e", "v0.1.1-0.20261023000000-ffffffffffff", -1},
	} {
		if got := Compare(testCase.left, testCase.right); (got < 0) != (testCase.expected < 0) || (got > 0) != (testCase.expected > 0) {
			t.Fatalf("%s vs %s: %d", testCase.left, testCase.right, got)
		}
	}
	if !IsRelease("v1.2.3") || !IsRelease("1.2.3") || IsRelease("v0.0.0-20261022-abc+dirty") || IsRelease("v0.1.1-0.20261022193356-161a1127359e") || IsRelease("v1.2.3+dirty") || IsRelease("unknown") || IsRelease("v1.2") {
		t.Fatalf("release detection")
	}
}
