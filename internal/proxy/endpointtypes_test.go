package proxy

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// TestEndpointTypeConstantsComeFromTheLeaf: every endpointType* constant in
// types.go must alias internal/endpointtype rather than carry its own literal.
// The log filter validates against endpointtype.All, so a family declared here
// as a literal would be stamped on rows the filter can never select.
func TestEndpointTypeConstantsComeFromTheLeaf(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "types.go", nil, 0)
	if err != nil {
		t.Fatalf("parse types.go: %v", err)
	}
	found := 0
	ast.Inspect(file, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, name := range vs.Names {
			if !strings.HasPrefix(name.Name, "endpointType") || i >= len(vs.Values) {
				continue
			}
			found++
			var pkg *ast.Ident
			if sel, ok := vs.Values[i].(*ast.SelectorExpr); ok {
				pkg, _ = sel.X.(*ast.Ident)
			}
			if pkg == nil || pkg.Name != "endpointtype" {
				t.Errorf("%s is not an endpointtype alias; declare the family in internal/endpointtype", name.Name)
			}
		}
		return true
	})
	if found == 0 {
		t.Fatal("found no endpointType* constants in types.go; this test can no longer see what it guards")
	}
}
