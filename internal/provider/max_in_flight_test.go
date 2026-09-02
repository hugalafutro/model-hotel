package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// The rule every write path shares.
func TestValidateMaxInFlight(t *testing.T) {
	for _, tc := range []struct {
		v    *int
		want bool
	}{
		{nil, true}, {new(1), true}, {new(MaxInFlightCeiling), true},
		{new(0), false}, {new(-5), false}, {new(MaxInFlightCeiling + 1), false},
	} {
		err := ValidateMaxInFlight(tc.v)
		if (err == nil) != tc.want {
			t.Fatalf("ValidateMaxInFlight(%v) = %v, want ok=%v", tc.v, err, tc.want)
		}
		if err != nil && !strings.Contains(err.Error(), "max_in_flight must be between 1 and 10000") {
			t.Fatalf("error text = %q", err)
		}
	}
}

// The column's CHECK constraint is the backstop for any write path that
// forgets the rule: an out-of-range value cannot be stored at all.
func TestMaxInFlightColumnBounds(t *testing.T) {
	pool := testDB.Pool()
	name := "bounds-" + uuid.New().String()[:8]
	if _, err := pool.Exec(context.Background(), `INSERT INTO providers (name, base_url, provider_type) VALUES ($1, 'https://b.example.test/v1', 'custom')`, name); err != nil {
		t.Fatalf("insert: %v", err)
	}
	for _, v := range []int{0, -5, MaxInFlightCeiling + 1} {
		if _, err := pool.Exec(context.Background(), `UPDATE providers SET max_in_flight = $1 WHERE name = $2`, v, name); err == nil {
			t.Fatalf("stored max_in_flight = %d past the constraint", v)
		} else if !strings.Contains(err.Error(), "providers_max_in_flight_bounds") {
			t.Fatalf("unexpected error for %d: %v", v, err)
		}
	}
	for _, v := range []int{1, MaxInFlightCeiling} {
		if _, err := pool.Exec(context.Background(), `UPDATE providers SET max_in_flight = $1 WHERE name = $2`, v, name); err != nil {
			t.Fatalf("in-range %d refused: %v", v, err)
		}
	}
	if _, err := pool.Exec(context.Background(), `UPDATE providers SET max_in_flight = NULL WHERE name = $1`, name); err != nil {
		t.Fatalf("null refused: %v", err)
	}
}
