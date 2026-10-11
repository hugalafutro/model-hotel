package db

import (
	"context"
	"io/fs"
	"strings"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/alert"
)

// quotaStatusMigration appends quota.status_lost and quota.status_restored to
// an operator's saved alert selection. Exercised directly, as 060 is, so each
// state an install can be in gets its own seeded run; the exhaustive edge
// cases of the shared SQL shape (whitespace, lookalikes, separators) live in
// TestQuotaSchemaDriftMigrationAppendsToASavedSelection and are not repeated.
const quotaStatusMigration = "migrations/096_quota_status_alerts.sql"

var quotaStatusEventTypes = []string{"quota.status_lost", "quota.status_restored"}

func readQuotaStatusMigration(t *testing.T) string {
	t.Helper()
	b, err := fs.ReadFile(embeddedMigrations, quotaStatusMigration)
	if err != nil {
		t.Fatalf("read %s: %v", quotaStatusMigration, err)
	}
	return string(b)
}

func TestQuotaStatusMigrationAppendsBothTypes(t *testing.T) {
	sql := readQuotaStatusMigration(t)
	ctx := context.Background()
	str := func(s string) *string { return &s }
	both := strings.Join(quotaStatusEventTypes, ",")

	cases := []struct {
		name     string
		seed     *string
		wantRow  bool
		wantCSV  string
		runTwice bool
	}{
		{name: "unset leaves no row", seed: nil, wantRow: false},
		{name: "saved selection gains both", seed: str("circuit_breaker.open"), wantRow: true, wantCSV: "circuit_breaker.open," + both},
		{name: "one already present gains only the other", seed: str("quota.status_restored,fleet.conflict"), wantRow: true, wantCSV: "quota.status_restored,fleet.conflict,quota.status_lost"},
		{name: "both present is untouched, twice", seed: str(both), wantRow: true, wantCSV: both, runTwice: true},
		{name: "explicitly empty selection is untouched", seed: str(""), wantRow: true, wantCSV: ""},
		{name: "the retired single type does not count as either", seed: str("quota.status_changed"), wantRow: true, wantCSV: "quota.status_changed," + both},
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
				t.Fatalf("row present = %v, want %v", found, tc.wantRow)
			}
			if found && got != tc.wantCSV {
				t.Errorf("alert_events = %q, want %q", got, tc.wantCSV)
			}
		})
	}
}

// TestQuotaStatusMigrationMatchesTheCatalog pins the migration to the catalog
// entries it propagates: both must exist and be DefaultOn.
func TestQuotaStatusMigrationMatchesTheCatalog(t *testing.T) {
	sql := readQuotaStatusMigration(t)
	for _, typ := range quotaStatusEventTypes {
		var found bool
		for _, e := range alert.Catalog() {
			if e.Type != typ {
				continue
			}
			found = true
			if !e.DefaultOn {
				t.Errorf("%s is not DefaultOn in the catalog, so the migration must not force it on", typ)
			}
		}
		if !found {
			t.Errorf("%s is not in alert.Catalog(); the migration would enable an unknown type", typ)
		}
		if !strings.Contains(sql, "'"+typ+"'") {
			t.Errorf("migration %s does not append %s", quotaStatusMigration, typ)
		}
	}
}
