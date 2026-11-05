package internal_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

const modulePath = "github.com/brendalima/despensa"

// forbidden lists, per layer, the direct imports AGENTS.md §2 rules out.
// A prefix match is used, so "github.com/jackc/pgx" covers every
// subpackage. The compiler will not catch a convenient but wrong import,
// so this does.
var forbidden = map[string][]string{
	"internal/core/domain": {
		modulePath + "/",
		"net/http", "database/sql", "github.com/jackc/pgx", "github.com/pressly/goose",
	},
	"internal/core/services": {
		modulePath + "/adapters", modulePath + "/internal/humanize",
		"net/http", "database/sql", "github.com/jackc/pgx",
	},
	"internal/repository": {
		modulePath + "/internal/core/services", modulePath + "/internal/humanize",
		"net/http",
	},
	// humanize is presentation and nothing else: it turns domain values
	// into Portuguese. An import of the service from here would make the
	// wording a layer that can also write to the database.
	"internal/humanize": {
		modulePath + "/adapters", modulePath + "/internal/core/services",
		modulePath + "/internal/repository",
		"net/http", "database/sql",
	},
}

func TestLayersRespectDependencyRules(t *testing.T) {
	for layer, banned := range forbidden {
		t.Run(layer, func(t *testing.T) {
			out, err := exec.Command("go", "list", "-f", `{{join .Imports "\n"}}`, modulePath+"/"+layer).Output()
			if err != nil {
				t.Skipf("couldn't run go list: %v", err)
			}

			for _, imp := range strings.Fields(string(out)) {
				for _, bad := range banned {
					if strings.HasPrefix(imp, bad) {
						t.Errorf("%s imports %s, which AGENTS.md forbids", layer, imp)
					}
				}
			}
		})
	}
}

// Configuration is read only in adapters/config.go and handed down
// through constructors.
func TestNoEnvironmentReadsInsideInternal(t *testing.T) {
	forEachSource(t, "..", func(t *testing.T, path string, file *ast.File, fset *token.FileSet) {
		if !strings.Contains(filepath.ToSlash(path), "/internal/") {
			return
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := call.X.(*ast.Ident)
			if ok && pkg.Name == "os" && call.Sel.Name == "Getenv" {
				t.Errorf("%s reads the environment: config belongs in adapters/config.go",
					fset.Position(call.Pos()))
			}
			return true
		})
	})
}

// The language boundary of AGENTS.md §12. The core holds no sentence
// meant for a person: a domain error carries a stable code, and
// internal/humanize owns the wording. Without this test the rule lasts
// until the first hurry.
//
// It looks for accented letters in string literals, walking the AST so
// that a comment written in Portuguese — which is most of them — is not
// mistaken for a message.
func TestNoPortugueseInsideTheCore(t *testing.T) {
	forEachSource(t, "core", func(t *testing.T, path string, file *ast.File, fset *token.FileSet) {
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			if accented(value) {
				t.Errorf("%s: accented string literal %q — user-facing text belongs in internal/humanize",
					fset.Position(lit.Pos()), value)
			}
			return true
		})
	})
}

// forEachSource parses every non-test .go file under root. Test files
// hold the fixtures — "taça", "pão de forma" — and those are data, which
// is exactly where Portuguese belongs.
func forEachSource(t *testing.T, root string, check func(*testing.T, string, *ast.File, *token.FileSet)) {
	t.Helper()
	fset := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		check(t, path, file, fset)
		return nil
	})
	if err != nil {
		t.Fatalf("couldn't walk %s: %v", root, err)
	}
}

func accented(s string) bool {
	for _, r := range s {
		if r > unicode.MaxASCII && unicode.IsLetter(r) {
			return true
		}
	}
	return false
}
