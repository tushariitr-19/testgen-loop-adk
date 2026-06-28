package workspace

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// ValidateTestFunc parses src as a single Go test function and verifies it
// has the canonical shape expected by `go test`:
//
//   - Exactly one top-level declaration
//   - That declaration is a function (not a method, not a var)
//   - Its name begins with "Test"
//   - It takes exactly one parameter of type *testing.T
//   - It returns no values
//
// On success it returns the function's name. Note: this function does not
// validate imports — the candidate must rely only on packages already
// imported by the test file.
func ValidateTestFunc(src string) (string, error) {
	fn, err := parseSingleFunc(src)
	if err != nil {
		return "", err
	}
	name := fn.Name.Name
	if !strings.HasPrefix(name, "Test") {
		return "", fmt.Errorf("test function name %q must start with Test", name)
	}
	if fn.Recv != nil {
		return "", fmt.Errorf("test function %q must not be a method", name)
	}
	if fn.Type.Params == nil || len(fn.Type.Params.List) != 1 {
		got := 0
		if fn.Type.Params != nil {
			got = len(fn.Type.Params.List)
		}
		return "", fmt.Errorf(
			"test function %q must take exactly one parameter, got %d", name, got)
	}
	if !isStarTestingT(fn.Type.Params.List[0].Type) {
		return "", fmt.Errorf(
			"test function %q parameter must be *testing.T", name)
	}
	if fn.Type.Results != nil && len(fn.Type.Results.List) > 0 {
		return "", fmt.Errorf("test function %q must not return values", name)
	}
	return name, nil
}

// ExtractTestNames parses a complete Go source file and returns the names of
// every top-level function whose name begins with "Test". It is used by
// AppendTest to detect name collisions before writing.
func ExtractTestNames(src string) ([]string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), "", src, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parsing source: %w", err)
	}
	var names []string
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if strings.HasPrefix(fn.Name.Name, "Test") {
			names = append(names, fn.Name.Name)
		}
	}
	return names, nil
}

// parseSingleFunc parses src as a single Go function declaration. The
// source is wrapped in a "package x" header so go/parser will accept it
// without the caller having to supply one.
func parseSingleFunc(src string) (*ast.FuncDecl, error) {
	src = strings.TrimSpace(src)
	if src == "" {
		return nil, errors.New("empty source")
	}
	wrapped := "package x\n" + src
	f, err := parser.ParseFile(
		token.NewFileSet(), "", wrapped, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parsing source: %w", err)
	}
	if len(f.Decls) != 1 {
		return nil, fmt.Errorf(
			"expected exactly one declaration, got %d", len(f.Decls))
	}
	fn, ok := f.Decls[0].(*ast.FuncDecl)
	if !ok {
		return nil, errors.New("declaration is not a function")
	}
	return fn, nil
}

func isStarTestingT(expr ast.Expr) bool {
	star, ok := expr.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return pkg.Name == "testing" && sel.Sel.Name == "T"
}
