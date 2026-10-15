package exchange

import (
	"archive/zip"
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

// writeRawZip writes entries with no manifest, for the tests that need a zip
// Write would never produce.
func writeRawZip(t *testing.T, out string, entries []Entry) {
	t.Helper()
	file, err := os.Create(out)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	writer := zip.NewWriter(file)
	for _, entry := range entries {
		err = add(writer, entry.Path, entry.Content)
		if err != nil {
			t.Fatalf("add: %v", err)
		}
	}
	err = writer.Close()
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	file.Close()
}

// writeRawZipWithManifest writes the manifest as given, without recomputing
// the hashes, so a tampered entry or a foreign version can be tested.
func writeRawZipWithManifest(t *testing.T, out string, manifest Manifest, entries []Entry) {
	t.Helper()
	content, err := yaml.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	all := append([]Entry{{Path: ManifestName, Content: content}}, entries...)
	writeRawZip(t, out, all)
}
