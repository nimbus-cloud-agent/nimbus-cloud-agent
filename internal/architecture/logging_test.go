package architecture_test

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

func TestProductionLogsUseSlog(t *testing.T) {
	for _, root := range []string{"..", "../../cmd"} {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || strings.Contains(filepath.ToSlash(path), "/generated/") || strings.Contains(filepath.ToSlash(path), "/testdb/") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return err
			}
			imports := map[string]string{}
			for _, imp := range file.Imports {
				pkg, _ := strconv.Unquote(imp.Path.Value)
				name := filepath.Base(pkg)
				if imp.Name != nil {
					name = imp.Name.Name
				}
				imports[name] = pkg
			}
			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				if name, ok := call.Fun.(*ast.Ident); ok && (name.Name == "print" || name.Name == "println") {
					t.Errorf("%s writes an unstructured log with %s", path, name.Name)
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkg, ok := selector.X.(*ast.Ident)
				if !ok {
					return true
				}
				if imports[pkg.Name] == "log" {
					t.Errorf("%s uses the standard logger instead of slog", path)
				}
				if imports[pkg.Name] == "fmt" && (selector.Sel.Name == "Print" || selector.Sel.Name == "Printf" || selector.Sel.Name == "Println") {
					t.Errorf("%s writes a log directly to stdout", path)
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
