// Package architecture holds no code, only the test that keeps the hexagonal
// layout honest: dependencies point inward, and nothing in the core knows the
// outside world.
package architecture

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const module = "github.com/Danidani23/agents-rein/internal/"

// allowed says which internal layers each layer may import.
var allowed = map[string][]string{
	"domain":   {"domain"},
	"ports":    {"domain"},
	"app":      {"domain", "ports"},
	"adapters": {"domain", "ports", "adapters/execute"},
	"cli":      {"domain", "ports", "app", "adapters", "version"},
	"version":  {},
}

func layerOf(importPath string) string {
	rest := strings.TrimPrefix(importPath, module)
	return strings.SplitN(rest, "/", 2)[0]
}

func TestDependenciesPointInward(t *testing.T) {
	root := filepath.Join("..")
	set := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		layer := strings.SplitN(filepath.ToSlash(relative), "/", 2)[0]
		if layer == "architecture" {
			return nil
		}
		file, err := parser.ParseFile(set, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, `"`)
			if !strings.HasPrefix(importPath, module) {
				continue
			}
			target := strings.TrimPrefix(importPath, module)
			permitted := false
			for _, candidate := range allowed[layer] {
				if target == candidate || strings.HasPrefix(target, candidate+"/") {
					permitted = true
				}
			}
			if !permitted {
				t.Errorf("%s (%s) imports %s; %s may import only %v", relative, layer, target, layer, allowed[layer])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}
