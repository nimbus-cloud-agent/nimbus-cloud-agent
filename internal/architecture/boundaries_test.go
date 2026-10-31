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

// Enforce dependency direction, not implementation text. New production files
// participate automatically; raw SQL fixtures stay inside the DAO test package.
func TestPersistenceAndSerializationBoundaries(t *testing.T) {
	root := ".."
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		pkg := strings.Split(filepath.ToSlash(rel), "/")[0]
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		for _, imp := range file.Imports {
			name, _ := strconv.Unquote(imp.Path.Value)
			if strings.HasPrefix(name, "github.com/matiasinsaurralde/go-e2b") && pkg != "sandbox" {
				t.Errorf("%s imports E2B outside the sandbox adapter", rel)
			}
			db := name == "database/sql" || strings.HasPrefix(name, "gorm.io/") || strings.HasPrefix(name, "github.com/jackc/")
			if db && pkg != "store" {
				t.Errorf("%s imports persistence driver %s outside DAO", rel, name)
			}
			if pkg == "model" && strings.Contains(name, "/internal/") {
				t.Errorf("application model depends on infrastructure: %s", name)
			}
		}
		if pkg == "server" {
			ast.Inspect(file, func(node ast.Node) bool {
				selector, ok := node.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				name, ok := selector.X.(*ast.Ident)
				if !ok || name.Name != "json" {
					return true
				}
				if selector.Sel.Name == "RawMessage" {
					t.Errorf("%s carries raw JSON in business code", rel)
				}
				if selector.Sel.Name == "Unmarshal" && filepath.Base(path) != "terminal.go" {
					t.Errorf("%s decodes data after the IO boundary", rel)
				}
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
