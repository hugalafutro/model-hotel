package api

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsUniqueViolation_PgError(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "23505"}
	if !isUniqueViolation(pgErr) {
		t.Error("Expected true for unique violation pg error")
	}
}

func TestIsUniqueViolation_OtherPgError(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "23503"}
	if isUniqueViolation(pgErr) {
		t.Error("Expected false for non-unique violation pg error")
	}
}

func TestIsUniqueViolation_NonPgError(t *testing.T) {
	if isUniqueViolation(errors.New("some error")) {
		t.Error("Expected false for non-pg error")
	}
}

func TestIsUniqueViolation_Nil(t *testing.T) {
	if isUniqueViolation(nil) {
		t.Error("Expected false for nil error")
	}
}

func TestIsUniqueViolation_Wrapped(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "23505"}
	wrapped := errors.Join(pgErr, errors.New("context"))
	if !isUniqueViolation(wrapped) {
		t.Error("Expected true for wrapped unique violation")
	}
}
