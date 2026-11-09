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
	// The tools are plain Go values. An import of the MCP SDK or of a
	// model's SDK from here would tie the six tools to one driver, and
	// the day the bot and Claude Desktop disagree about what "taça" is
	// worth is the day the inventory is wrong.
	"internal/agent": {
		modulePath + "/adapters", modulePath + "/internal/repository",
		"net/http", "database/sql",
		"github.com/jackc/pgx", "github.com/modelcontextprotocol/go-sdk",
		"github.com/anthropics/anthropic-sdk-go",
	},
	// The bot is a driver: it may know HTTP and the model's SDK, and it
	// must not know persistence. The pantry reaches it as a service
	// interface, so a rule cannot grow in the conversation layer.
	"internal/bot": {
		modulePath + "/adapters", modulePath + "/internal/repository",
		"database/sql", "github.com/jackc/pgx",
		"github.com/anthropics/anthropic-sdk-go",
	},
	// The model adapter is the only place that imports the SDK, and it
	// knows nothing about where a message came from or where it goes.
	"internal/bot/claude": {
		modulePath + "/adapters", modulePath + "/internal/repository",
		modulePath + "/internal/bot/whatsapp",
		"database/sql", "github.com/jackc/pgx",
	},
	// The transcriber is a third adapter, and the only importer of
	// OpenAI. It hears bytes and answers words: it must not know the
	// provider the audio came from, the model that reads it, or the
	// pantry it ends up writing to.
	"internal/bot/whisper": {
		modulePath + "/adapters", modulePath + "/internal/repository",
		modulePath + "/internal/bot/claude", modulePath + "/internal/bot/whatsapp",
		modulePath + "/internal/core/services",
		"database/sql", "github.com/jackc/pgx",
		"github.com/anthropics/anthropic-sdk-go",
	},
	// The provider adapter is the other half: HTTP and Meta's payloads,
	// with no model and no pantry in it.
	"internal/bot/whatsapp": {
		modulePath + "/adapters", modulePath + "/internal/repository",
		modulePath + "/internal/core/services", modulePath + "/internal/bot/claude",
		"database/sql", "github.com/jackc/pgx",
		"github.com/anthropics/anthropic-sdk-go",
	},
	"internal/rest": {
		modulePath + "/adapters", modulePath + "/internal/repository",
		"database/sql", "github.com/jackc/pgx",
		"github.com/anthropics/anthropic-sdk-go",
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

// The language boundary of AGENTS.md §12. Without this test the rule
// lasts until the first hurry. It walks the AST rather than the lines so
// a Portuguese comment is not mistaken for a message.
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

// The other half of the language boundary: the Portuguese a model or a
// person reads is wording, and wording lives in one file per package.
// The tool descriptions are prompt — the two sentences that invert what
// a model would otherwise do are the product — so they belong with the
// rest of the wording and not scattered through the handlers.
func TestPortugueseLivesInOneFilePerPackage(t *testing.T) {
	wording := map[string]string{
		"agent":    "descriptions.go",
		"bot":      "prompt.go",
		"humanize": "", // the whole package is wording
	}

	forEachSource(t, "..", func(t *testing.T, path string, file *ast.File, fset *token.FileSet) {
		dir := filepath.Base(filepath.Dir(path))
		allowed, watched := wording[dir]
		if !watched || allowed == "" || filepath.Base(path) == allowed {
			return
		}

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
				t.Errorf("%s: accented string literal %q — the wording of %s belongs in %s",
					fset.Position(lit.Pos()), value, dir, allowed)
			}
			return true
		})
	})
}

// forEachSource parses every non-test .go file under root. Test files
// hold the fixtures, and those are data.
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
