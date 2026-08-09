package frontdesk

import (
	"maps"
	"strings"
	"testing"
)

// sectionsLike returns a full per-section hash map with the named sections
// overridden, so a stub member can differ from the primary in exactly those.
func sectionsLike(overrides map[string]string) map[string]string {
	out := map[string]string{
		"providers":       "sec-p",
		"virtual_keys":    "sec-v",
		"settings":        "sec-s",
		"failover_groups": "sec-g",
		"users":           "sec-u",
		"disabled_models": "sec-d",
	}
	maps.Copy(out, overrides)
	return out
}

// TestAutoSync_EventNamesTheDifferingSections: the config.auto_synced roll-up
// names WHICH config sections the re-synced member differed in, from the
// per-section hashes both version reads already carry, so the operator can see
// what a repair actually repaired instead of a bare "did not hold the primary's
// config".
func TestAutoSync_EventNamesTheDifferingSections(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) {
		r.versionHash = "hash-drifted"
		r.appliedHash = "hash-B"
		r.dryDiff = driftDiff
		r.versionSections = sectionsLike(map[string]string{
			"failover_groups": "sec-g-drift",
			"disabled_models": "sec-d-drift",
		})
	})
	f.primary.mu.Lock()
	f.primary.versionSections = sectionsLike(nil)
	f.primary.mu.Unlock()

	f.tick(t)

	evs, _, err := f.store.ListEvents(t.Context(), EventFilter{Type: "config.auto_synced"})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("config.auto_synced events = %d, want 1", len(evs))
	}
	const want = "Auto-synced 1 member: did not hold the primary's config (replica: failover groups, disabled models)"
	if evs[0].Message != want {
		t.Errorf("message = %q, want %q", evs[0].Message, want)
	}

	members, ok := evs[0].Metadata["members"].([]any)
	if !ok || len(members) != 1 {
		t.Fatalf("metadata members = %#v, want a list with the one synced member", evs[0].Metadata["members"])
	}
	entry, ok := members[0].(map[string]any)
	if !ok {
		t.Fatalf("metadata member entry = %#v, want an object", members[0])
	}
	if entry["member_id"] != f.replicaM.ID {
		t.Errorf("metadata member_id = %v, want %q", entry["member_id"], f.replicaM.ID)
	}
	if entry["name"] != "replica" {
		t.Errorf("metadata name = %v, want %q", entry["name"], "replica")
	}
	secs, ok := entry["sections"].([]any)
	if !ok || len(secs) != 2 || secs[0] != "failover_groups" || secs[1] != "disabled_models" {
		t.Errorf("metadata sections = %#v, want [failover_groups disabled_models] in payload order", entry["sections"])
	}
}

// TestAutoSync_EventWithoutSectionsKeepsThePlainMessage: a member (or primary)
// running an older app version answers the version read without section hashes.
// The roll-up then reads exactly as it always has, and the metadata still names
// the synced member, just without sections.
func TestAutoSync_EventWithoutSectionsKeepsThePlainMessage(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) {
		r.versionHash = "hash-drifted"
		r.appliedHash = "hash-B"
		r.dryDiff = driftDiff
	})

	f.tick(t)

	evs, _, err := f.store.ListEvents(t.Context(), EventFilter{Type: "config.auto_synced"})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("config.auto_synced events = %d, want 1", len(evs))
	}
	const want = "Auto-synced 1 member: did not hold the primary's config"
	if evs[0].Message != want {
		t.Errorf("message = %q, want %q", evs[0].Message, want)
	}
	members, ok := evs[0].Metadata["members"].([]any)
	if !ok || len(members) != 1 {
		t.Fatalf("metadata members = %#v, want a list with the one synced member", evs[0].Metadata["members"])
	}
	entry, ok := members[0].(map[string]any)
	if !ok {
		t.Fatalf("metadata member entry = %#v, want an object", members[0])
	}
	if entry["name"] != "replica" {
		t.Errorf("metadata name = %v, want %q", entry["name"], "replica")
	}
	if _, present := entry["sections"]; present {
		t.Errorf("metadata sections = %#v, want absent when neither side serves section hashes", entry["sections"])
	}
}

// TestAutoSync_OldMemberWithoutSectionsDegradesToThePlainMessage pins the
// asymmetric half of the degradation contract, which is also the realistic
// rolling-upgrade shape: a current primary serves section hashes while an older
// member does not. With only one side to compare, the honest claim is no detail
// at all; naming every section as differing would be the "everything differs"
// lie the comparison must never tell.
func TestAutoSync_OldMemberWithoutSectionsDegradesToThePlainMessage(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) {
		r.versionHash = "hash-drifted"
		r.appliedHash = "hash-B"
		r.dryDiff = driftDiff
		// No versionSections: an older member answers with the overall hash alone.
	})
	f.primary.mu.Lock()
	f.primary.versionSections = sectionsLike(nil)
	f.primary.mu.Unlock()

	f.tick(t)

	evs, _, err := f.store.ListEvents(t.Context(), EventFilter{Type: "config.auto_synced"})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("config.auto_synced events = %d, want 1", len(evs))
	}
	const want = "Auto-synced 1 member: did not hold the primary's config"
	if evs[0].Message != want {
		t.Errorf("message = %q, want %q", evs[0].Message, want)
	}
}

// TestDescribeSectionDetails_MultiMember pins the roll-up's member separator and
// the mixed case a fleet-wide pass produces: members with section detail join
// with "; " (a comma would read as one member with many sections), a member
// without detail is left out, and a detail carrying only unlabelled keys never
// renders a dangling "name: ".
func TestDescribeSectionDetails_MultiMember(t *testing.T) {
	got := describeSectionDetails([]syncedMemberDetail{
		{id: "1", name: "hotel-2", sections: []string{"providers", "virtual_keys"}},
		{id: "2", name: "hotel-3", sections: nil},
		{id: "3", name: "hotel-4", sections: []string{"disabled_models"}},
		{id: "4", name: "hotel-5", sections: []string{"not-a-section"}},
	})
	const want = "hotel-2: providers, virtual keys; hotel-4: disabled models"
	if got != want {
		t.Errorf("describeSectionDetails = %q, want %q", got, want)
	}
}

// TestAutoSync_SectionsMatchingOverallDivergedStaysPlain: a member whose overall
// hash differs while every known section matches (a future section this build
// does not know, or a transient read skew) must not fabricate a detail: the
// message stays plain rather than claiming an empty difference.
func TestAutoSync_SectionsMatchingOverallDivergedStaysPlain(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) {
		r.versionHash = "hash-drifted"
		r.appliedHash = "hash-B"
		r.dryDiff = driftDiff
		r.versionSections = sectionsLike(nil)
	})
	f.primary.mu.Lock()
	f.primary.versionSections = sectionsLike(nil)
	f.primary.mu.Unlock()

	f.tick(t)

	evs, _, err := f.store.ListEvents(t.Context(), EventFilter{Type: "config.auto_synced"})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("config.auto_synced events = %d, want 1", len(evs))
	}
	if strings.Contains(evs[0].Message, "(") {
		t.Errorf("message = %q, want no section detail when no known section differs", evs[0].Message)
	}
}
