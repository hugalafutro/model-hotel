package db

import (
	"context"
	"io/fs"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/alert"
)

// quotaSchemaDriftMigration adds quota.schema_drift to an operator's saved alert
// selection. It is exercised directly (rather than via runMigrations, which has
// already applied it to the shared test database) so each of the states an
// existing install can be in gets its own seeded run.
const quotaSchemaDriftMigration = "migrations/060_quota_schema_drift_alert.sql"

const quotaDriftEventType = "quota.schema_drift"

// readQuotaSchemaDriftMigration returns the embedded migration's SQL.
func readQuotaSchemaDriftMigration(t *testing.T) string {
	t.Helper()
	b, err := fs.ReadFile(embeddedMigrations, quotaSchemaDriftMigration)
	if err != nil {
		t.Fatalf("read %s: %v", quotaSchemaDriftMigration, err)
	}
	return string(b)
}

// seedAlertEvents puts the settings table into one of the states an install can
// be in: absent (seed=nil) or holding a specific saved CSV.
func seedAlertEvents(t *testing.T, seed *string) {
	t.Helper()
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `DELETE FROM settings WHERE key = 'alert_events'`); err != nil {
		t.Fatalf("clear alert_events: %v", err)
	}
	if seed != nil {
		if _, err := testPool.Exec(ctx,
			`INSERT INTO settings (key, value, updated_at) VALUES ('alert_events', $1, now())`, *seed); err != nil {
			t.Fatalf("seed alert_events: %v", err)
		}
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM settings WHERE key = 'alert_events'`)
	})
}

// readAlertEvents returns the stored CSV and whether the row exists.
func readAlertEvents(t *testing.T) (string, bool) {
	t.Helper()
	rows, err := testPool.Query(context.Background(),
		`SELECT value FROM settings WHERE key = 'alert_events'`)
	if err != nil {
		t.Fatalf("read alert_events: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return "", false
	}
	var v string
	if serr := rows.Scan(&v); serr != nil {
		t.Fatalf("scan alert_events: %v", serr)
	}
	return v, true
}

// TestQuotaSchemaDriftMigrationAppendsToASavedSelection covers every state an
// install can be in when the migration runs.
//
// The reason this migration exists: alert.DefaultEnabledCSV() only applies when
// alert_events is *unset*, so marking the catalog entry DefaultOn reaches fresh
// installs only. Any operator who has ever saved a picker selection would never
// receive an event whose whole purpose is to report a silent failure.
func TestQuotaSchemaDriftMigrationAppendsToASavedSelection(t *testing.T) {
	sql := readQuotaSchemaDriftMigration(t)
	ctx := context.Background()

	str := func(s string) *string { return &s }

	cases := []struct {
		name     string
		seed     *string
		wantRow  bool
		wantCSV  string
		because  string
		runTwice bool
	}{
		{
			name:    "unset leaves no row",
			seed:    nil,
			wantRow: false,
			because: "an install with no saved selection already gets the event from DefaultEnabledCSV; creating a row here would freeze its selection at today's defaults",
		},
		{
			name:    "saved selection is appended to",
			seed:    str("circuit_breaker.open,failover.sync_error"),
			wantRow: true,
			wantCSV: "circuit_breaker.open,failover.sync_error," + quotaDriftEventType,
			because: "the operator's existing choices must survive verbatim, in order, with the new type appended",
		},
		{
			name:     "already present is untouched and appending twice does not duplicate",
			seed:     str("circuit_breaker.open," + quotaDriftEventType + ",fleet.conflict"),
			wantRow:  true,
			wantCSV:  "circuit_breaker.open," + quotaDriftEventType + ",fleet.conflict",
			because:  "the guard must be idempotent, including against a mid-CSV occurrence",
			runTwice: true,
		},
		{
			name:    "explicitly empty selection is untouched",
			seed:    str(""),
			wantRow: true,
			wantCSV: "",
			because: "an empty value means the operator deselected everything; appending would switch an alert back on against their choice",
		},
		{
			name:    "a longer lookalike type does not count as present",
			seed:    str(quotaDriftEventType + "_v2"),
			wantRow: true,
			wantCSV: quotaDriftEventType + "_v2," + quotaDriftEventType,
			because: "the presence check must match whole CSV entries, not substrings",
		},
		{
			name:    "whitespace around entries still counts as present",
			seed:    str("circuit_breaker.open, " + quotaDriftEventType),
			wantRow: true,
			wantCSV: "circuit_breaker.open, " + quotaDriftEventType,
			because: "ParseEnabled trims entries, so a hand-edited CSV with spaces must not gain a duplicate",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seedAlertEvents(t, tc.seed)

			runs := 1
			if tc.runTwice {
				runs = 2
			}
			for range runs {
				if _, err := testPool.Exec(ctx, sql); err != nil {
					t.Fatalf("apply migration: %v", err)
				}
			}

			got, found := readAlertEvents(t)
			if found != tc.wantRow {
				t.Fatalf("row present = %v, want %v (%s)", found, tc.wantRow, tc.because)
			}
			if found && got != tc.wantCSV {
				t.Errorf("alert_events = %q, want %q (%s)", got, tc.wantCSV, tc.because)
			}
		})
	}
}

// TestQuotaSchemaDriftMigrationMatchesTheCatalog pins the migration to the
// catalog it exists to propagate. A migration that appended a type the catalog
// does not carry, or that was left behind when the entry stopped being
// default-on, would silently write an entry the picker cannot even render.
func TestQuotaSchemaDriftMigrationMatchesTheCatalog(t *testing.T) {
	var found bool
	for _, e := range alert.Catalog() {
		if e.Type != quotaDriftEventType {
			continue
		}
		found = true
		if !e.DefaultOn {
			t.Errorf("%s is not DefaultOn in the catalog, so the migration must not force it on", e.Type)
		}
	}
	if !found {
		t.Fatalf("%s is not in alert.Catalog(); the migration would enable an unknown type", quotaDriftEventType)
	}
	if sql := readQuotaSchemaDriftMigration(t); !containsToken(sql, quotaDriftEventType) {
		t.Errorf("migration %s does not mention %s", quotaSchemaDriftMigration, quotaDriftEventType)
	}
}

// containsToken reports whether s contains sub.
func containsToken(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
