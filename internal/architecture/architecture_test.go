// Package architecture holds no code, only the test that keeps the layout
// honest: dependencies point inward, the gateway knows the orchestration and
// the services only through interfaces, a service knows no other service, and
// the core knows no adapter. main, outside internal, is the composition root
// and may import everything. The rules are written out in docs/architecture.md.
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
// libraries are the pure packages of 2_services that anything may import; a
// service proper may import them and the ports, never another service.
var libraries = []string{"2_services/address", "2_services/declaration", "2_services/lint", "2_services/exchange", "2_services/machine"}

var allowed = map[string][]string{
	"2_services":      append([]string{"ports"}, libraries...),
	"ports":           libraries,
	"1_orchestration": {"2_services", "ports"},
	"adapters":        append([]string{"ports", "adapters/execute"}, libraries...),
	"0_gateway":       append([]string{"ports", "1_orchestration", "adapters/terminal", "adapters/execute", "version"}, libraries...),
	"version":         {},
}

// layerOf names the layer of a path under internal/: the three application
// layers sit under app/, the edges beside it.
func layerOf(rest string) string {
	rest = strings.TrimPrefix(rest, "app/")
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
		layer := layerOf(filepath.ToSlash(relative))
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
			target := strings.TrimPrefix(strings.TrimPrefix(importPath, module), "app/")
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
