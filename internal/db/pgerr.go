package db

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// IsUniqueViolation reports whether err is a PostgreSQL unique-constraint
// violation (SQLSTATE 23505). Callers use it to turn a racing insert into a
// 409/duplicate response instead of a 500. The SQLite Front Desk store has its
// own variant: the driver reports a different error shape entirely.
func IsUniqueViolation(err error) bool {
	return pgCode(err, "23505")
}

// IsUniqueViolationOn reports whether err is a unique violation raised by one
// named constraint or index. A table can carry several, so a caller that
// explains the failure in its own words has to know which one fired: reading
// every 23505 as the interesting one starts lying the day another unique column
// is added.
func IsUniqueViolationOn(err error, constraint string) bool {
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		return pgErr.Code == "23505" && pgErr.ConstraintName == constraint
	}
	return false
}

// IsRaisedException reports whether err is an exception a PL/pgSQL block raised
// on purpose with RAISE EXCEPTION (SQLSTATE P0001), which is how a migration
// refuses an install it will not upgrade. Everything the database rejects on
// its own carries its own SQLSTATE, so this is what separates a decision from a
// failure.
func IsRaisedException(err error) bool {
	return pgCode(err, "P0001")
}

// IsForeignKeyViolation reports whether err is a PostgreSQL foreign-key
// violation (SQLSTATE 23503). Callers use it to turn a reference to a row that
// has since been deleted into a 400/404 instead of a 500.
func IsForeignKeyViolation(err error) bool {
	return pgCode(err, "23503")
}

// pgCode reports whether err carries the given SQLSTATE.
func pgCode(err error, code string) bool {
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		return pgErr.Code == code
	}
	return false
}
