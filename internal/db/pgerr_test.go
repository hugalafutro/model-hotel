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

func TestIsForeignKeyViolation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil_error", nil, false},
		{"pg_error_23503_fk_violation", &pgconn.PgError{Code: "23503"}, true},
		{"pg_error_23505_unique_violation", &pgconn.PgError{Code: "23505"}, false},
		{"wrapped_pg_error_23503", fmt.Errorf("wrap: %w", &pgconn.PgError{Code: "23503"}), true},
		{"non_pg_error", errors.New("some other error"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsForeignKeyViolation(tt.err); got != tt.want {
				t.Errorf("IsForeignKeyViolation(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// IsUniqueViolationOn is the narrow variant: the same 23505, but only from the
// constraint the caller names, so a message about one index cannot be attached
// to another index's failure.
func TestIsUniqueViolationOn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		constraint string
		want       bool
	}{
		{"nil_error", nil, "providers_name_normalized_unique", false},
		{"matching_constraint", &pgconn.PgError{Code: "23505", ConstraintName: "providers_name_normalized_unique"}, "providers_name_normalized_unique", true},
		{"other_constraint", &pgconn.PgError{Code: "23505", ConstraintName: "providers_name_unique"}, "providers_name_normalized_unique", false},
		{"no_constraint_name", &pgconn.PgError{Code: "23505"}, "providers_name_normalized_unique", false},
		{"other_sqlstate", &pgconn.PgError{Code: "23503", ConstraintName: "providers_name_normalized_unique"}, "providers_name_normalized_unique", false},
		{"wrapped", fmt.Errorf("wrap: %w", &pgconn.PgError{Code: "23505", ConstraintName: "providers_name_normalized_unique"}), "providers_name_normalized_unique", true},
		{"non_pg_error", errors.New("some other error"), "providers_name_normalized_unique", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsUniqueViolationOn(tt.err, tt.constraint); got != tt.want {
				t.Errorf("IsUniqueViolationOn(%v, %q) = %v, want %v", tt.err, tt.constraint, got, tt.want)
			}
		})
	}
}

func TestIsRaisedException(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil_error", err: nil, want: false},
		{name: "plain_error", err: errors.New("boom"), want: false},
		{name: "raise_exception_P0001", err: &pgconn.PgError{Code: "P0001"}, want: true},
		{name: "wrapped_raise_exception", err: fmt.Errorf("migration: %w", &pgconn.PgError{Code: "P0001"}), want: true},
		{name: "undefined_table_42P01", err: &pgconn.PgError{Code: "42P01"}, want: false},
		{name: "unique_violation_23505", err: &pgconn.PgError{Code: "23505"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsRaisedException(tt.err); got != tt.want {
				t.Errorf("IsRaisedException(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
