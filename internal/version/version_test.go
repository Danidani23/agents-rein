package version

import "testing"

func TestString(t *testing.T) {
	if String() == "" {
		t.Fatalf("no version")
	}
	tagged = "v0.1.0"
	defer func() { tagged = "" }()
	if String() != "v0.1.0" {
		t.Fatalf("tagged build: %s", String())
	}
}
