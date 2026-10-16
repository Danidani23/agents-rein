package exchange

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, root string, relative string, content string) {
	t.Helper()
	fullPath := filepath.Join(root, relative)
	err := os.MkdirAll(filepath.Dir(fullPath), 0o755)
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	err = os.WriteFile(fullPath, []byte(content), 0o644)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
}

func source(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "global/AGENTS.md", "# Global\n")
	writeFile(t, root, "skills/commit/SKILL.md", "---\nname: commit\ndescription: d\n---\n")
	writeFile(t, root, "skills/commit/references/notes.md", "notes\n")
	writeFile(t, root, "skills/adr/SKILL.md", "---\nname: adr\ndescription: d\n---\n")
	return root
}

func paths(entries []Entry) []string {
	var all []string
	for _, entry := range entries {
		all = append(all, entry.Path)
	}
	return all
}

func TestFromUserLevelSource(t *testing.T) {
	t.Run("the instructions become rules/global.md and every skill file copies", func(t *testing.T) {
		entries, err := FromUserLevelSource(source(t))
		if err != nil {
			t.Fatalf("err %v", err)
		}
		got := strings.Join(paths(entries), " ")
		if got != "rules/global.md skills/adr/SKILL.md skills/commit/SKILL.md skills/commit/references/notes.md" {
			t.Fatalf("paths %q", got)
		}
		rules, skills := Count(entries)
		if rules != 1 || skills != 2 {
			t.Fatalf("rules %d skills %d", rules, skills)
		}
	})
	t.Run("a folder without instructions or skills is refused", func(t *testing.T) {
		_, err := FromUserLevelSource(t.TempDir())
		if err == nil {
			t.Fatalf("expected an error")
		}
	})
}

func TestWriteAndRead(t *testing.T) {
	t.Run("a written zip reads back with the same entries and hash", func(t *testing.T) {
		entries, _ := FromUserLevelSource(source(t))
		out := filepath.Join(t.TempDir(), "export.zip")
		err := Write(out, Manifest{Kind: "user", User: "dani", Source: "test", Created: "2026-10-15T10:00:00Z", ToolVersion: "test"}, entries)
		if err != nil {
			t.Fatalf("write: %v", err)
		}
		manifest, read, err := Read(out)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if manifest.User != "dani" || manifest.FormatVersion != FormatVersion || len(manifest.Files) != 4 {
			t.Fatalf("manifest %+v", manifest)
		}
		if Hash(read) != Hash(entries) || strings.Join(paths(read), " ") != strings.Join(paths(entries), " ") {
			t.Fatalf("entries differ: %v vs %v", paths(read), paths(entries))
		}
	})
	t.Run("the same content written twice gives the same bytes", func(t *testing.T) {
		entries, _ := FromUserLevelSource(source(t))
		manifest := Manifest{Kind: "user", User: "dani", Source: "test", Created: "2026-10-15T10:00:00Z", ToolVersion: "test"}
		first := filepath.Join(t.TempDir(), "a.zip")
		second := filepath.Join(t.TempDir(), "b.zip")
		_ = Write(first, manifest, entries)
		_ = Write(second, manifest, entries)
		a, _ := os.ReadFile(first)
		b, _ := os.ReadFile(second)
		if string(a) != string(b) {
			t.Fatalf("bytes differ")
		}
	})
	t.Run("a zip without the manifest is not a rein export", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "plain.zip")
		err := Write(out, Manifest{}, []Entry{{Path: "rules/x.md", Content: []byte("x")}})
		if err != nil {
			t.Fatalf("write: %v", err)
		}
		// strip the manifest by rewriting the zip from its entries only
		_, entries, _ := Read(out)
		raw := filepath.Join(t.TempDir(), "raw.zip")
		writeRawZip(t, raw, entries)
		_, _, err = Read(raw)
		if err == nil || !strings.Contains(err.Error(), "not a rein export") {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("a changed file fails its hash", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "export.zip")
		_ = Write(out, Manifest{}, []Entry{{Path: "rules/x.md", Content: []byte("x")}})
		manifest, entries, _ := Read(out)
		entries[0].Content = []byte("tampered")
		tampered := filepath.Join(t.TempDir(), "tampered.zip")
		writeRawZipWithManifest(t, tampered, manifest, entries)
		_, _, err := Read(tampered)
		if err == nil || !strings.Contains(err.Error(), "does not match its hash") {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("a newer format version is refused", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "export.zip")
		_ = Write(out, Manifest{}, []Entry{{Path: "rules/x.md", Content: []byte("x")}})
		manifest, entries, _ := Read(out)
		manifest.FormatVersion = FormatVersion + 1
		newer := filepath.Join(t.TempDir(), "newer.zip")
		writeRawZipWithManifest(t, newer, manifest, entries)
		_, _, err := Read(newer)
		if err == nil || !strings.Contains(err.Error(), "format version") {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("unpack writes every entry under the directory", func(t *testing.T) {
		directory := t.TempDir()
		err := Unpack(directory, []Entry{{Path: "rules/x.md", Content: []byte("x")}, {Path: "skills/s/SKILL.md", Content: []byte("y")}})
		if err != nil {
			t.Fatalf("unpack: %v", err)
		}
		content, err := os.ReadFile(filepath.Join(directory, "skills", "s", "SKILL.md"))
		if err != nil || string(content) != "y" {
			t.Fatalf("content %q err %v", content, err)
		}
	})
}

func TestFromFolder(t *testing.T) {
	t.Run("the toolkit layout is recognised", func(t *testing.T) {
		entries, err := FromFolder(source(t))
		if err != nil || len(entries) != 4 || entries[0].Path != "rules/global.md" {
			t.Fatalf("entries %v err %v", paths(entries), err)
		}
	})
	t.Run("the declarations layout is recognised", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "rules/style.md", "# Style\n")
		writeFile(t, root, "skills/deploy/SKILL.md", "---\nname: deploy\ndescription: d\n---\n")
		entries, err := FromFolder(root)
		if err != nil || strings.Join(paths(entries), " ") != "rules/style.md skills/deploy/SKILL.md" {
			t.Fatalf("entries %v err %v", paths(entries), err)
		}
	})
	t.Run("an empty folder is refused", func(t *testing.T) {
		_, err := FromFolder(t.TempDir())
		if err == nil {
			t.Fatalf("expected an error")
		}
	})
}
