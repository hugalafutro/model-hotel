package db

import "fmt"

// SetList collects an UPDATE's SET clauses and their positional arguments.
// First is the number of the first placeholder Arg hands out, so a caller
// that reserves $1 for the row id starts at 2 and prepends the id itself.
type SetList struct {
	First   int
	Clauses []string
	Args    []any
}

// Arg records v and returns its placeholder.
func (s *SetList) Arg(v any) string {
	s.Args = append(s.Args, v)
	return fmt.Sprintf("$%d", s.First+len(s.Args)-1)
}

// Set adds "col = $n" bound to v.
func (s *SetList) Set(col string, v any) {
	s.Add(col + " = " + s.Arg(v))
}

// Add appends clauses that take no argument.
func (s *SetList) Add(clauses ...string) {
	s.Clauses = append(s.Clauses, clauses...)
}
