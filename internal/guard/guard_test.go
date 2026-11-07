// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package guard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/karamble/goarmin/pkg/httpx"
)

// repoRoot walks up from the test's directory to the directory holding go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}

// walk visits every file under the repository root, skipping build output and dependencies.
func walk(t *testing.T, fn func(path, rel string)) {
	t.Helper()
	root := repoRoot(t)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "dist", "bin":
				return filepath.SkipDir
			}
			return nil
		}
		fn(path, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestNoRawHTTPClients fails when code outside pkg/httpx uses net/http's package-level client
// helpers or builds its own http.Client: those would send Go's default User-Agent, or one a
// library chooses, instead of the browser User-Agent required for every outbound request.
func TestNoRawHTTPClients(t *testing.T) {
	forbiddenFuncs := map[string]bool{"Get": true, "Head": true, "Post": true, "PostForm": true, "DefaultClient": true}
	walk(t, func(path, rel string) {
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") || strings.HasPrefix(rel, "pkg/httpx/") {
			return
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Errorf("%s: %v", rel, err)
			return
		}
		httpName := ""
		for _, imp := range f.Imports {
			if p, _ := strconv.Unquote(imp.Path.Value); p == "net/http" {
				httpName = "http"
				if imp.Name != nil {
					httpName = imp.Name.Name
				}
			}
		}
		if httpName == "" {
			return
		}
		isHTTP := func(e ast.Expr, name string) bool {
			sel, ok := e.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != name {
				return false
			}
			id, ok := sel.X.(*ast.Ident)
			return ok && id.Name == httpName
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.SelectorExpr:
				if id, ok := x.X.(*ast.Ident); ok && id.Name == httpName && forbiddenFuncs[x.Sel.Name] {
					t.Errorf("%s: uses %s.%s; create clients with pkg/httpx", fset.Position(x.Pos()), httpName, x.Sel.Name)
				}
			case *ast.CompositeLit:
				if isHTTP(x.Type, "Client") {
					t.Errorf("%s: constructs an http.Client; use httpx.NewClient", fset.Position(x.Pos()))
				}
			case *ast.CallExpr:
				if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "new" && len(x.Args) == 1 && isHTTP(x.Args[0], "Client") {
					t.Errorf("%s: new(http.Client); use httpx.NewClient", fset.Position(x.Pos()))
				}
			}
			return true
		})
	})
}

// Date literals in tests make them pass the day they are written and fail weeks later, when a
// fixture date meets the real clock. Tests must derive every timestamp from time.Now().
var datePatterns = []*regexp.Regexp{
	regexp.MustCompile(`time\.Date\(\s*(19|20)\d\d\b`),
	regexp.MustCompile("[\"'`](19|20)\\d\\d-[01]\\d-[0-3]\\d"),
	regexp.MustCompile(`time\.Unix(Milli|Micro|Nano)?\(\s*\d{9,}`),
	// The dashboard's tests (TypeScript): new Date(2026, …), Date.UTC(2026, …), epoch literals.
	regexp.MustCompile(`(new Date|Date\.UTC)\(\s*(19|20)\d\d\b`),
	regexp.MustCompile(`new Date\(\s*\d{12,}`),
}

// isTest reports whether rel is a test file (Go or the dashboard's).
func isTest(rel string) bool {
	return strings.HasSuffix(rel, "_test.go") || strings.HasSuffix(rel, ".test.ts") || strings.HasSuffix(rel, ".test.tsx")
}

func TestNoHardcodedDatesInTests(t *testing.T) {
	walk(t, func(path, rel string) {
		inTestdata := strings.Contains("/"+rel, "/testdata/")
		if !isTest(rel) && !inTestdata {
			return
		}
		if strings.HasPrefix(rel, "internal/guard/") { // this file spells out the patterns
			return
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: %v", rel, err)
			return
		}
		for i, line := range strings.Split(string(data), "\n") {
			for _, re := range datePatterns {
				if re.MatchString(line) {
					t.Errorf("%s:%d: hardcoded date (%s); derive it from time.Now()", rel, i+1, strings.TrimSpace(line))
				}
			}
		}
	})
}

// The guards themselves must catch what they are meant to catch.
func TestGuardPatternsMatch(t *testing.T) {
	samples := []string{
		"t0 := time.Date(" + "2026, 1, 2, 0, 0, 0, 0, time.UTC)",
		`ts := "` + "2026-09-21" + `T12:00:00Z"`,
		"t0 := time.Unix(" + "1790000000, 0)",
		"const d = new Date(" + "2026, 0, 2)",
		"const day = '" + "2026-01-02'",
		"const t = Date.UTC(" + "2026, 0, 2)",
	}
	for _, s := range samples {
		hit := false
		for _, re := range datePatterns {
			hit = hit || re.MatchString(s)
		}
		if !hit {
			t.Errorf("no date pattern matches %q", s)
		}
	}
	for _, ok := range []string{"now := time.Now()", "time.Unix(0, 0)", `fmt.Sprintf("%d-%02d", y, m)`} {
		for _, re := range datePatterns {
			if re.MatchString(ok) {
				t.Errorf("pattern %s wrongly matches %q", re, ok)
			}
		}
	}
}

// TestWebNpmUserAgent: npm reaches the registry only with the browser User-Agent.
func TestWebNpmUserAgent(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "web", ".npmrc"))
	if os.IsNotExist(err) {
		t.Skip("no web/ yet")
	}
	if err != nil {
		t.Fatal(err)
	}
	want := "user-agent=" + httpx.BrowserUA
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == want {
			return
		}
	}
	t.Fatalf("web/.npmrc must contain %q (pkg/httpx.BrowserUA)", want)
}

// webURLAllowed lists the only absolute URLs the dashboard's code may contain: the browser
// never contacts a third party, and the CSP would block it anyway.
var webURLAllowed = []string{
	"http://localhost",                        // tests parsing relative URLs
	"https://www.openstreetmap.org/copyright", // the map attribution link (opened by the user)
}

func TestNoThirdPartyURLsInWeb(t *testing.T) {
	re := regexp.MustCompile(`https?://[^\s'"` + "`" + `)<>]+`)
	walk(t, func(path, rel string) {
		if !strings.HasPrefix(rel, "web/") || !(strings.HasPrefix(rel, "web/src/") || rel == "web/index.html") {
			return
		}
		switch filepath.Ext(rel) {
		case ".ts", ".tsx", ".css", ".html":
		default:
			return
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: %v", rel, err)
			return
		}
		for i, line := range strings.Split(string(data), "\n") {
			for _, u := range re.FindAllString(line, -1) {
				ok := false
				for _, a := range webURLAllowed {
					ok = ok || strings.HasPrefix(u, a)
				}
				if !ok {
					t.Errorf("%s:%d: absolute URL %s; the dashboard talks only to its own origin", rel, i+1, u)
				}
			}
		}
	})
}
