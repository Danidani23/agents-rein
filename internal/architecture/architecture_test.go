// Package architecture holds no code, only the test that keeps the layout
// honest: dependencies point inward, the gateway knows the orchestration and
// the services only through interfaces, a service knows no other service, and
// the core knows no adapter. main, outside internal, is the composition root
// and may import everything. The rules are written out in docs/architecture.md.
package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
)

const module = "github.com/Danidani23/agents-rein/internal/"

// allowed says which internal layers each layer may import.
// libraries are the pure packages of 2_services that anything may import; a
// service proper may import them and the ports, never another service.
var libraries = []string{"2_services/address", "2_services/declaration", "2_services/lint", "2_services/exchange", "2_services/machine", "2_services/refusal", "2_services/scaffold", "2_services/distill", "2_services/proposal", "2_services/resolve", "2_services/compile", "2_services/semver"}

// fakes is the one adapter a test file of any layer may import: the
// in-memory implementations of the ports.
const fakes = "adapters/fake"

var allowed = map[string][]string{
	"2_services":      append([]string{"ports"}, libraries...),
	"ports":           libraries,
	"1_orchestration": {"2_services", "ports"},
	"adapters":        append([]string{"ports", "adapters/execute"}, libraries...),
	"0_gateway":       append([]string{"ports", "1_orchestration", "0_gateway/mcp", "adapters/terminal", "adapters/execute", "version"}, libraries...),
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
	fileSet := token.NewFileSet()
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
		isTest := strings.HasSuffix(relative, "_test.go")
		file, err := parser.ParseFile(fileSet, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, importSpec := range file.Imports {
			importPath := strings.Trim(importSpec.Path.Value, `"`)
			if !strings.HasPrefix(importPath, module) {
				continue
			}
			target := strings.TrimPrefix(strings.TrimPrefix(importPath, module), "app/")
			permitted := isTest && target == fakes
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

// TestOneWorkflowPerFile keeps the orchestration folder readable as a list of
// workflows: every method of Orchestrator lives either in queries.go or in
// its own file named workflow_<name>.go after the method, and a workflow file
// holds no other method.
func TestOneWorkflowPerFile(t *testing.T) {
	folder := filepath.Join("..", "app", "1_orchestration")
	fileSet := token.NewFileSet()
	packages, err := parser.ParseDir(fileSet, folder, func(info os.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	methodFiles := map[string][]string{}
	for _, parsedPackage := range packages {
		for path, file := range parsedPackage.Files {
			for _, declaration := range file.Decls {
				functionDeclaration, ok := declaration.(*ast.FuncDecl)
				if ok && functionDeclaration.Recv != nil && receiverName(functionDeclaration.Recv) == "Orchestrator" {
					methodFiles[filepath.Base(path)] = append(methodFiles[filepath.Base(path)], functionDeclaration.Name.Name)
				}
			}
		}
	}
	if len(methodFiles) == 0 {
		t.Fatalf("no methods of Orchestrator found in %s", folder)
	}
	for name, methods := range methodFiles {
		if name == "queries.go" {
			continue
		}
		if !strings.HasPrefix(name, "workflow_") {
			t.Errorf("%s defines %v; a method of Orchestrator is a query in queries.go or a workflow in workflow_<name>.go", name, methods)
			continue
		}
		if len(methods) != 1 {
			t.Errorf("%s holds %v; a workflow file holds exactly one workflow", name, methods)
			continue
		}
		expected := "workflow_" + snakeCase(methods[0]) + ".go"
		if name != expected {
			t.Errorf("%s defines %s; its file is %s", name, methods[0], expected)
		}
	}
}

// receiverName is the type name of a method receiver, pointer or not.
func receiverName(receiver *ast.FieldList) string {
	if len(receiver.List) != 1 {
		return ""
	}
	switch expression := receiver.List[0].Type.(type) {
	case *ast.StarExpr:
		identifier, ok := expression.X.(*ast.Ident)
		if ok {
			return identifier.Name
		}
	case *ast.Ident:
		return expression.Name
	}
	return ""
}

// snakeCase turns ConnectOrganization into connect_organization.
func snakeCase(name string) string {
	var builder strings.Builder
	for index, character := range name {
		if unicode.IsUpper(character) && index > 0 {
			builder.WriteByte('_')
		}
		builder.WriteRune(unicode.ToLower(character))
	}
	return builder.String()
}
