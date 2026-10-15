package gextto

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestInternalJSONPathsAreRegisteredRoutes fails when a v2 page reads or writes
// through v2InternalJSON with a method and path the router does not serve. The
// call is HTTP in-process, so a renamed or removed API route does not break the
// build: the page would only degrade at runtime to «Caricamento non riuscito».
//
// Each call is resolved against the real router: string literals are kept and
// every non-literal piece of a concatenated path becomes a placeholder segment.
// Paths that come from a table or editor spec (Endpoint/GetPath/PostPath/
// TestEndpoint fields) are checked too.
func TestInternalJSONPathsAreRegisteredRoutes(t *testing.T) {
	state := newTestAppState(t)
	Router(state)
	mux := state.internalMux.Load()
	if mux == nil {
		t.Fatal("Router did not register the internal router on the state")
	}
	resolves := func(method, path string) bool {
		request, err := http.NewRequest(method, path, nil)
		if err != nil {
			return false
		}
		_, pattern := mux.Handler(request)
		return pattern != "" && pattern != "GET /{$}"
	}

	files, _ := filepath.Glob("*.go")
	fset := token.NewFileSet()
	var missing []string
	checked := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) < 3 {
				return true
			}
			if ident, ok := call.Fun.(*ast.Ident); !ok || ident.Name != "v2InternalJSON" {
				return true
			}
			method, ok := internalJSONMethod(call.Args[1])
			if !ok {
				return true // method chosen at runtime
			}
			path, ok := internalJSONPath(call.Args[2])
			if !ok {
				return true // path built elsewhere (spec fields are checked below)
			}
			checked++
			if !resolves(method, path) {
				missing = append(missing, fset.Position(call.Pos()).String()+" -> "+method+" "+path)
			}
			return true
		})
	}

	specPath := regexp.MustCompile(`\b(Endpoint|GetPath|PostPath|TestEndpoint|AddPath):\s*"(/api/[^"]+)"`)
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") || !strings.HasPrefix(file, "uiweb") {
			continue
		}
		raw := readFileForTest(t, file)
		for _, m := range specPath.FindAllStringSubmatch(raw, -1) {
			checked++
			// A table Endpoint is read with GET, a search Endpoint with POST.
			methods := []string{http.MethodGet, http.MethodPost}
			switch m[1] {
			case "GetPath":
				methods = methods[:1]
			case "PostPath", "TestEndpoint", "AddPath":
				methods = methods[1:]
			}
			ok := false
			for _, method := range methods {
				ok = ok || resolves(method, m[2])
			}
			if !ok {
				missing = append(missing, file+" "+m[1]+" -> "+strings.Join(methods, "|")+" "+m[2])
			}
		}
	}
	if len(missing) > 0 {
		t.Errorf("v2 pages call API routes that are not registered:\n  %s", strings.Join(missing, "\n  "))
	}
	if checked < 30 {
		t.Fatalf("only %d internal API calls resolved: the source scan no longer matches v2InternalJSON", checked)
	}
}

func internalJSONMethod(expr ast.Expr) (string, bool) {
	switch e := expr.(type) {
	case *ast.SelectorExpr:
		if pkg, ok := e.X.(*ast.Ident); ok && pkg.Name == "http" && strings.HasPrefix(e.Sel.Name, "Method") {
			return strings.ToUpper(strings.TrimPrefix(e.Sel.Name, "Method")), true
		}
	case *ast.BasicLit:
		if value, err := strconv.Unquote(e.Value); err == nil {
			return value, true
		}
	}
	return "", false
}

// internalJSONPath rebuilds a sample path from a literal or a "+" chain that
// starts with a literal; every non-literal operand becomes "x".
func internalJSONPath(expr ast.Expr) (string, bool) {
	var parts []string
	var walk func(ast.Expr) bool
	walk = func(e ast.Expr) bool {
		switch v := e.(type) {
		case *ast.BinaryExpr:
			return v.Op == token.ADD && walk(v.X) && walk(v.Y)
		case *ast.BasicLit:
			value, err := strconv.Unquote(v.Value)
			if err != nil {
				return false
			}
			parts = append(parts, value)
		default:
			parts = append(parts, "x")
		}
		return true
	}
	if !walk(expr) || len(parts) == 0 || !strings.HasPrefix(parts[0], "/api/") {
		return "", false
	}
	return strings.Join(parts, ""), true
}

func readFileForTest(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}
