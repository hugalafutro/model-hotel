package db

import (
	"reflect"
	"testing"
)

func TestSetListNumbersPlaceholdersFromFirst(t *testing.T) {
	t.Parallel()
	s := SetList{First: 2}
	s.Set("a", 1)
	s.Add("b = NULL", "c = now()")
	s.Add("d = d || " + s.Arg("x"))
	wantClauses := []string{"a = $2", "b = NULL", "c = now()", "d = d || $3"}
	if !reflect.DeepEqual(s.Clauses, wantClauses) {
		t.Errorf("clauses = %v, want %v", s.Clauses, wantClauses)
	}
	if want := []any{1, "x"}; !reflect.DeepEqual(s.Args, want) {
		t.Errorf("args = %v, want %v", s.Args, want)
	}
}
