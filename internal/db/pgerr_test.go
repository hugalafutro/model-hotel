package db

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsUniqueViolation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil_error",
			err:  nil,
			want: false,
		},
		{
			name: "pg_error_23505_unique_violation",
			err:  &pgconn.PgError{Code: "23505"},
			want: true,
		},
		{
			name: "pg_error_23503_fk_violation",
			err:  &pgconn.PgError{Code: "23503"},
			want: false,
		},
		{
			name: "pg_error_42P01_undefined_table",
			err:  &pgconn.PgError{Code: "42P01"},
			want: false,
		},
		{
			name: "wrapped_pg_error_23505",
			err:  fmt.Errorf("wrap: %w", &pgconn.PgError{Code: "23505"}),
			want: true,
		},
		{
			name: "non_pg_error",
			err:  errors.New("some other error"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsUniqueViolation(tt.err); got != tt.want {
				t.Errorf("IsUniqueViolation(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
