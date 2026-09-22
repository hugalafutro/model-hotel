package proxy

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// TestEndpointTypesCoversEveryConstant ties the endpointType* constants to the
// EndpointTypes slice built from them. The log filter in internal/api validates
// against that slice and ignores anything it does not recognise, so a family
// that is declared and stamped on rows but left out of the slice becomes a
// filter option that matches every row instead of its own. That is the defect
// this file exists to prevent recurring, and neither the compiler nor a
// hand-written list of families would catch it: the constants are only reachable
// by reading the declaration, so the test reads it.
func TestEndpointTypesCoversEveryConstant(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "types.go", nil, 0)
	if err != nil {
		t.Fatalf("parse types.go: %v", err)
	}

	declared := map[string]string{} // constant name -> its string value
	ast.Inspect(file, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, name := range vs.Names {
			if !strings.HasPrefix(name.Name, "endpointType") || i >= len(vs.Values) {
				continue
			}
			lit, ok := vs.Values[i].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			value, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatalf("unquote %s: %v", name.Name, err)
			}
			declared[name.Name] = value
		}
		return true
	})

	if len(declared) == 0 {
		t.Fatal("found no endpointType* constants in types.go; this test can no longer see what it guards")
	}
	for name, value := range declared {
		if !slices.Contains(EndpointTypes, value) {
			t.Errorf("constant %s (%q) is missing from EndpointTypes, so the log filter would ignore it and return every row", name, value)
		}
	}
	if len(EndpointTypes) != len(declared) {
		t.Errorf("EndpointTypes has %d entries but types.go declares %d endpointType* constants", len(EndpointTypes), len(declared))
	}
}
