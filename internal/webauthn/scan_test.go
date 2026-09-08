package webauthn

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

// errScanner stands in for a corrupt or schema-mismatched row: every Scan fails
// with the configured error.
type errScanner struct{ err error }

func (s errScanner) Scan(...any) error { return s.err }

// scanHelpers pairs each row-scanning helper with a name so both are held to
// the same error contract.
var scanHelpers = []struct {
	name string
	scan func(scanner) error
}{
	{"credential", func(r scanner) error { _, err := scanCredential(r); return err }},
	{"session", func(r scanner) error { _, err := scanSession(r); return err }},
}

// TestScanHelpers_PropagateScanError pins that only pgx.ErrNoRows becomes
// ErrNotFound: any other scan failure is not translated, so a corrupt row is
// never mistaken for a missing one. A helper that translated
// this error would fail the check, since the sentinel it would return does not
// wrap want.
func TestScanHelpers_PropagateScanError(t *testing.T) {
	want := errors.New("column type mismatch")
	for _, h := range scanHelpers {
		t.Run(h.name, func(t *testing.T) {
			if err := h.scan(errScanner{err: want}); !errors.Is(err, want) {
				t.Fatalf("scan error = %v, want %v", err, want)
			}
		})
	}
}

// TestScanHelpers_NoRowsBecomesNotFound pins the one scan error the helpers do
// translate, so callers can branch on ErrNotFound.
func TestScanHelpers_NoRowsBecomesNotFound(t *testing.T) {
	for _, h := range scanHelpers {
		t.Run(h.name, func(t *testing.T) {
			if err := h.scan(errScanner{err: pgx.ErrNoRows}); !errors.Is(err, ErrNotFound) {
				t.Fatalf("scan error = %v, want ErrNotFound", err)
			}
		})
	}
}
