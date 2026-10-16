package github

import "testing"

func TestSlugOf(t *testing.T) {
	if SlugOf("Danidani23") != "danidani23" {
		t.Fatalf("slug %q", SlugOf("Danidani23"))
	}
}
