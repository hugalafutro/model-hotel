package api

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/uuid"
)

func encodeCursorJSON(t *testing.T, raw string) string {
	t.Helper()
	return base64.StdEncoding.EncodeToString([]byte(raw))
}

// TestCursorDecodeRejectsNonUUIDID covers both keyset cursor decoders. The id
// they carry is compared against a uuid column, so a base64-valid cursor
// holding anything else must be rejected here rather than reaching Postgres
// and surfacing as a 500.
func TestCursorDecodeRejectsNonUUIDID(t *testing.T) {
	valid := uuid.NewString()
	cases := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{"model cursor valid", `{"sort_by":"name","id":"` + valid + `"}`, false},
		{"model cursor non-uuid", `{"sort_by":"name","id":"not-a-uuid"}`, true},
		{"model cursor empty id", `{"sort_by":"name"}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var c modelCursor
			err := c.decode(encodeCursorJSON(t, tc.raw))
			if (err != nil) != tc.wantErr {
				t.Fatalf("decode(%s) err = %v, wantErr %v", tc.raw, err, tc.wantErr)
			}
		})
	}

	logCases := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{"log cursor valid", `{"created_at":"2026-01-01T00:00:00Z","id":"` + valid + `"}`, false},
		{"log cursor non-uuid", `{"created_at":"2026-01-01T00:00:00Z","id":"1 OR 1=1"}`, true},
		{"log cursor empty id", `{"created_at":"2026-01-01T00:00:00Z"}`, true},
	}
	for _, tc := range logCases {
		t.Run(tc.name, func(t *testing.T) {
			var c logCursor
			err := c.decode(encodeCursorJSON(t, tc.raw))
			if (err != nil) != tc.wantErr {
				t.Fatalf("decode(%s) err = %v, wantErr %v", tc.raw, err, tc.wantErr)
			}
		})
	}

	// A body that is not base64 and a body that is not JSON still fail first.
	var c logCursor
	if err := c.decode("!!!not base64!!!"); err == nil {
		t.Error("invalid base64 should be an error")
	}
	if err := c.decode(encodeCursorJSON(t, "not json")); err == nil {
		t.Error("invalid JSON should be an error")
	}
}

// TestParseProviderIDFilterRejectsMalformed pins the provider_id filter to
// all-or-nothing parsing. Dropping a malformed value silently broadens the
// result set, and dropping every value turns a restrictive request into one
// that returns the whole catalogue.
func TestParseProviderIDFilterRejectsMalformed(t *testing.T) {
	a, b := uuid.New(), uuid.New()

	w := httptest.NewRecorder()
	ids, ok := parseProviderIDFilter(w, a.String()+","+b.String())
	if !ok {
		t.Fatalf("two valid ids rejected: %s", w.Body.String())
	}
	if len(ids) != 2 || ids[0] != a || ids[1] != b {
		t.Errorf("ids = %v, want [%s %s]", ids, a, b)
	}

	if ids, ok = parseProviderIDFilter(httptest.NewRecorder(), ""); !ok || ids != nil {
		t.Errorf("empty filter: ids = %v ok = %v, want nil true", ids, ok)
	}

	for _, raw := range []string{"nope", a.String() + ",nope", "nope," + a.String()} {
		w = httptest.NewRecorder()
		if _, ok = parseProviderIDFilter(w, raw); ok {
			t.Errorf("parseProviderIDFilter(%q) accepted a non-UUID value", raw)
		}
		if w.Code != http.StatusBadRequest {
			t.Errorf("parseProviderIDFilter(%q) status = %d, want 400", raw, w.Code)
		}
	}
}

// TestParseModelListParamsRejectsMalformedProviderID checks the entry point
// answers 400 before any query is built.
func TestParseModelListParamsRejectsMalformedProviderID(t *testing.T) {
	w := httptest.NewRecorder()
	if _, ok := parseModelListParams(w, url.Values{"provider_id": {"not-a-uuid"}}); ok {
		t.Fatal("parseModelListParams accepted a malformed provider_id")
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}

	id := uuid.New()
	p, ok := parseModelListParams(httptest.NewRecorder(), url.Values{"provider_id": {id.String()}})
	if !ok {
		t.Fatal("a valid provider_id was rejected")
	}
	if len(p.providerIDs) != 1 || p.providerIDs[0] != id {
		t.Errorf("providerIDs = %v, want [%s]", p.providerIDs, id)
	}

	// The parsed ids reach the SQL as one array argument.
	conds, args := buildModelFilterConditions(url.Values{}, p)
	if len(conds) != 1 || len(args) != 1 {
		t.Fatalf("conditions = %v args = %v, want one of each", conds, args)
	}
	got, isSlice := args[0].([]uuid.UUID)
	if !isSlice || len(got) != 1 || got[0] != id {
		t.Errorf("filter arg = %v, want []uuid.UUID{%s}", args[0], id)
	}
}
