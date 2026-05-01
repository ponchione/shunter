package shunter

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestProductionLoggingStaysRuntimeScoped(t *testing.T) {
	allowedSlogFiles := map[string]struct{}{
		"observability.go": {},
	}
	allowedGlobalOutputFiles := map[string]struct{}{
		"cmd/shunter/main.go": {},
	}
	processGlobalLogCalls := map[string]struct{}{
		"Default":   {},
		"Fatal":     {},
		"Fatalf":    {},
		"Fatalln":   {},
		"New":       {},
		"Output":    {},
		"Panic":     {},
		"Panicf":    {},
		"Panicln":   {},
		"Print":     {},
		"Printf":    {},
		"Println":   {},
		"SetFlags":  {},
		"SetOutput": {},
		"SetPrefix": {},
		"Writer":    {},
	}

	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "reference", "vendor", "node_modules":
				return filepath.SkipDir
			default:
				return nil
			}
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}

		logImportNames := map[string]struct{}{}
		osImportNames := map[string]struct{}{}
		for _, imported := range file.Imports {
			importPath, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return err
			}
			importName := importLocalName(imported, importPath)
			switch importPath {
			case "log":
				t.Errorf("%s imports process-global log; use runtime-scoped observability", path)
				logImportNames[importName] = struct{}{}
			case "log/slog":
				if _, ok := allowedSlogFiles[filepath.ToSlash(path)]; !ok {
					t.Errorf("%s imports log/slog directly; route production logs through runtimeObservability", path)
				}
			case "os":
				osImportNames[importName] = struct{}{}
			}
		}

		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := selector.X.(*ast.Ident)
			if !ok {
				return true
			}
			if _, ok := logImportNames[ident.Name]; ok {
				if _, ok := processGlobalLogCalls[selector.Sel.Name]; ok {
					t.Errorf("%s calls log.%s; use runtime-scoped observability", fset.Position(selector.Pos()), selector.Sel.Name)
				}
				return true
			}
			if _, ok := osImportNames[ident.Name]; ok {
				if selector.Sel.Name != "Stdout" && selector.Sel.Name != "Stderr" {
					return true
				}
				if _, ok := allowedGlobalOutputFiles[filepath.ToSlash(path)]; !ok {
					t.Errorf("%s uses os.%s directly; use injected writers at CLI boundaries", fset.Position(selector.Pos()), selector.Sel.Name)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func importLocalName(imported *ast.ImportSpec, importPath string) string {
	if imported.Name != nil {
		return imported.Name.Name
	}
	return filepath.Base(importPath)
}
