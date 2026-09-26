package endpointtype

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"testing"
)

// TestAllCoversEveryConstant ties the family constants to the slice All builds
// from them. The log filter in internal/api validates against All and ignores
// anything it does not recognise, so a family that is declared and stamped on
// rows but left out of All becomes a filter option that matches every row
// instead of its own. Neither the compiler nor a hand-written list of families
// would catch that: the constants are only reachable by reading the
// declaration, so the test reads it.
func TestAllCoversEveryConstant(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "endpointtype.go", nil, 0)
	if err != nil {
		t.Fatalf("parse endpointtype.go: %v", err)
	}

	declared := map[string]string{} // constant name -> its string value
	ast.Inspect(file, func(n ast.Node) bool {
		gd, ok := n.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			return true
		}
		for _, spec := range gd.Specs {
			vs := spec.(*ast.ValueSpec)
			for i, name := range vs.Names {
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
		}
		return false
	})

	if len(declared) == 0 {
		t.Fatal("found no constants in endpointtype.go; this test can no longer see what it guards")
	}
	all := All()
	for name, value := range declared {
		if !slices.Contains(all, value) {
			t.Errorf("constant %s (%q) is missing from All, so the log filter would ignore it and return every row", name, value)
		}
	}
	if len(all) != len(declared) {
		t.Errorf("All has %d entries but endpointtype.go declares %d constants", len(all), len(declared))
	}
}

// TestAllReturnsACopy: a caller that edits the slice it was handed must not
// change what the next caller reads.
func TestAllReturnsACopy(t *testing.T) {
	first := All()
	first[0] = "tampered"
	if All()[0] != Chat {
		t.Fatal("All shares its backing array: one caller's edit leaked into the next")
	}
}
