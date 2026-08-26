package frontdesk

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// incompleteState is what Front Desk remembers about a member it has given the
// primary's config to: enough to bound the re-push, describe the divergence, and
// know whether the member counts as diverged.
type incompleteState struct {
	// lastAttempt is when this member last committed a config Front Desk pushed.
	// Zero means "push on the next pass", and doubles as the "has not had its
	// chance yet" signal that keeps an unreached member from being flagged.
	lastAttempt time.Time
	// lastUnapplied names the custom failover groups the member reported it could
	// not build, so an alert raised on a later pass can still be specific. Empty
	// when it named none, which is also what an unexplained divergence looks like.
	lastUnapplied []string
	// lastPartial names the custom failover groups the member built with fewer
	// entries than the primary sent, so the alert can say which group is short.
	lastPartial []string
	// lastUnappliedModels names the per-model disables the member could not apply
	// for want of the model, as provider/model_id. It is what explains a hash that
	// keeps differing on a member whose groups are all fine.
	lastUnappliedModels []string
	// diverged is true once the member is known not to hold the primary's config:
	// either its own hash was measured and differed after it took the config, or its
	// hash has been unreadable long enough that convergence can no longer be
	// established. The fleet badge and the edge-triggered alert read this; a member
	// merely pushed to is not diverged.
	diverged bool
	// unreadableSince is when this member's config hash first failed to read, with
	// every read since having failed too. Zero once one succeeds.
	unreadableSince time.Time
	// lastReadErr is why the most recent hash read failed, carried into the alert so
	// it names the cause rather than only the symptom.
	lastReadErr string
	// divergedKind is which of the two the last emitted alert described. The badge
	// is the same either way, but the alert bodies contradict each other, so a
	// member that moves from one to the other has to say so.
	divergedKind divergenceKind
}

// divergenceKind is how a member came to be known not to hold the primary's
// config: measured against its own hash, or never measurable at all.
type divergenceKind int

const (
	// divergedMeasured: the member's hash was read and differed.
	divergedMeasured divergenceKind = iota
	// divergedUnmeasurable: the member's hash could not be read at all.
	divergedUnmeasurable
)

// recordSyncAttempt remembers that a member received a config Front Desk pushed,
// with whatever it reported it could not build or built short. The stamp bounds the
// re-push (shouldSkipIncompleteRetry) and marks the member as having had its chance
// (hasBeenPushedSinceReset).
func (s *Server) recordSyncAttempt(memberID string, unapplied, partial, unappliedModels []string) {
	s.syncIncompleteMu.Lock()
	defer s.syncIncompleteMu.Unlock()
	st := s.syncIncomplete[memberID]
	st.lastAttempt = time.Now()
	st.lastUnapplied = unapplied
	st.lastPartial = partial
	st.lastUnappliedModels = unappliedModels
	s.syncIncomplete[memberID] = st
}

// markUnconfirmedPush remembers that a member's latest real config push, carrying
// the primary config identified by hash, got no usable answer, so its last-sync
// marker could not be stamped even though the import may have completed
// member-side. A later pass that measures the member holding exactly that hash
// stamps the marker then and clears this. An empty hash records nothing: the
// caller had no hash for what it pushed (the wizard when the primary's hash was
// unreadable), and a stamp that cannot be tied to the pushed config must not be
// promised.
func (s *Server) markUnconfirmedPush(memberID, hash string) {
	if hash == "" {
		return
	}
	s.syncIncompleteMu.Lock()
	defer s.syncIncompleteMu.Unlock()
	s.unconfirmedSync[memberID] = hash
}

// clearUnconfirmedPush forgets a member's unconfirmed push of exactly this
// config once a sync stamp has landed for it: either a later push of it was
// confirmed and stamped itself, or the verification pass stamped on the hash
// match. Compare-and-delete rather than delete: between the caller reading the
// flag and stamping, a concurrent push (the wizard runs on its own goroutine)
// can lose ITS answer and re-mark the member with a newer hash, and that entry
// must survive to earn its own stamp.
func (s *Server) clearUnconfirmedPush(memberID, hash string) {
	s.syncIncompleteMu.Lock()
	defer s.syncIncompleteMu.Unlock()
	if hash != "" && s.unconfirmedSync[memberID] == hash {
		delete(s.unconfirmedSync, memberID)
	}
}

// hasUnconfirmedPush reports whether this member has a lost-answer push of
// exactly this config behind it, still waiting for the stamp its lost answer
// denied it. Bound to the hash so a push of an older config can never be
// "proven" by convergence on a newer one, and a definite member-side failure
// (nothing committed) can never be stamped just because the member later came to
// hold the primary's config some other way.
func (s *Server) hasUnconfirmedPush(memberID, hash string) bool {
	s.syncIncompleteMu.Lock()
	defer s.syncIncompleteMu.Unlock()
	return hash != "" && s.unconfirmedSync[memberID] == hash
}

// hasBeenPushedSinceReset reports whether this member has received the config
// since the last reset (a primary edit or the enable-time kick). This is the whole
// no-flap guard: a member not yet reached must never be flagged for diverging.
func (s *Server) hasBeenPushedSinceReset(memberID string) bool {
	s.syncIncompleteMu.Lock()
	defer s.syncIncompleteMu.Unlock()
	return !s.syncIncomplete[memberID].lastAttempt.IsZero()
}

// flagDiverged marks a member as not holding the primary's config and returns a
// copy of its state along with whether it was already flagged, so the caller emits
// the transition event exactly once. The slices are copied and are always lists
// rather than null, so consumers see one shape and the caller reads them outside
// the lock.
func (s *Server) flagDiverged(memberID string, kind divergenceKind) (incompleteState, bool) {
	s.syncIncompleteMu.Lock()
	defer s.syncIncompleteMu.Unlock()
	st := s.syncIncomplete[memberID]
	// Edge-triggered on the flag AND on which kind it is: a member that stops being
	// measurable, or becomes measurable and is then found to differ, has a new thing
	// to say. Without the second half the operator's last alert would keep insisting
	// the member "cannot be measured ... unknown" after Front Desk had measured it
	// and could name the groups, which is the same over-claiming in reverse.
	already := st.diverged && st.divergedKind == kind
	st.diverged = true
	st.divergedKind = kind
	s.syncIncomplete[memberID] = st
	st.lastUnapplied = append([]string{}, st.lastUnapplied...)
	st.lastPartial = append([]string{}, st.lastPartial...)
	st.lastUnappliedModels = append([]string{}, st.lastUnappliedModels...)
	return st, already
}

// markMemberIncomplete records a member whose own config hash differs from the
// primary's after it took that config, and emits config.sync_incomplete once on the
// transition in. Edge-triggered: the member is re-checked every pass, so a
// level-triggered event would alert on each one until it converged.
func (s *Server) markMemberIncomplete(ctx context.Context, m *Member) {
	st, already := s.flagDiverged(m.ID, divergedMeasured)
	if already {
		return
	}
	s.emit(ctx, Event{
		Type: "config.sync_incomplete", Severity: "warning", Source: "frontdesk",
		Message:  divergenceMessage(m.Name, st.lastUnapplied, st.lastPartial, st.lastUnappliedModels),
		MemberID: m.ID,
		Metadata: map[string]any{
			"unapplied": st.lastUnapplied, "partial": st.lastPartial,
			"unapplied_models": st.lastUnappliedModels, "unreadable": false,
		},
	})
}

// markMemberUnmeasured records a member whose config hash has been unreadable past
// unreadableHashThreshold, and emits config.sync_incomplete once on the transition
// in, exactly as a measured divergence does.
//
// It shares that event type and the amber badge deliberately. Both mean the same
// thing to an operator: this member is not known to hold the primary's config, and
// its routing cannot be trusted to match. Splitting them would add a wire code
// every consumer must learn to say something the message and the unreadable flag
// already say. The distinction that matters, measured wrong versus not measurable,
// is in the message and the metadata.
func (s *Server) markMemberUnmeasured(ctx context.Context, m *Member) {
	st, already := s.flagDiverged(m.ID, divergedUnmeasurable)
	if already {
		return
	}
	s.emit(ctx, Event{
		Type: "config.sync_incomplete", Severity: "warning", Source: "frontdesk",
		Message:  unmeasuredMessage(m.Name, st.lastReadErr),
		MemberID: m.ID,
		// The name lists stay present and empty: nothing was reported unbuilt here,
		// and one shape per event type keeps consumers from special-casing.
		Metadata: map[string]any{
			"unapplied": st.lastUnapplied, "partial": st.lastPartial,
			"unapplied_models": st.lastUnappliedModels,
			"unreadable":       true, "error": st.lastReadErr,
			"unreadable_since": st.unreadableSince,
		},
	})
}

// unreadableCause renders why a hash read failed, for an alert body. A transport
// failure arrives as a *url.Error carrying the member's full URL, and this cause
// is rendered into the config.sync_incomplete message, which is the field the
// Apprise dispatcher sends offsite (internal/alert.dispatcher uses ev.Message as
// the body). Member names already travel in events; their addresses do not, and a
// LAN hostname and port should not start reaching a notification provider as a
// side effect of this check. The unredacted error still goes to the local log.
func unreadableCause(err error) string {
	if _, ok := errors.AsType[*url.Error](err); ok {
		if isTimeout(err) {
			return "the member did not answer in time"
		}
		return "the member could not be reached"
	}
	return err.Error()
}

// recordUnreadableHash notes that a member's config hash could not be read, and
// reports whether it has now been unreadable long enough to flag the member. The
// first failure only starts the clock, so one slow read never alerts.
func (s *Server) recordUnreadableHash(memberID, cause string, now time.Time) bool {
	s.syncIncompleteMu.Lock()
	defer s.syncIncompleteMu.Unlock()
	st := s.syncIncomplete[memberID]
	if st.unreadableSince.IsZero() {
		st.unreadableSince = now
	}
	st.lastReadErr = cause
	s.syncIncomplete[memberID] = st
	return now.Sub(st.unreadableSince) >= unreadableHashThreshold
}

// clearUnreadableHash stops a member's unreadable clock once a read succeeds. It
// leaves the rest of the member's state alone: a hash that reads but differs is a
// measured divergence, whose retry timer and reported group names must survive.
func (s *Server) clearUnreadableHash(memberID string) {
	s.syncIncompleteMu.Lock()
	defer s.syncIncompleteMu.Unlock()
	st, ok := s.syncIncomplete[memberID]
	if !ok || st.unreadableSince.IsZero() {
		return
	}
	st.unreadableSince = time.Time{}
	st.lastReadErr = ""
	s.syncIncomplete[memberID] = st
}

// divergenceMessageMaxNames bounds how many group names each clause of
// divergenceMessage spells out, so a member short across dozens of groups cannot
// produce a message dozens of names long. The event Metadata carries the full lists
// untruncated.
const divergenceMessageMaxNames = 5

// joinCapped renders names as a comma-separated list, capped at limit entries,
// with a trailing count of however many were left off.
func joinCapped(names []string, limit int) string {
	if len(names) <= limit {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s, and %d more", strings.Join(names[:limit], ", "), len(names)-limit)
}

// divergenceMessage renders the operator-facing reason a member does not hold the
// primary's config, from the two things it can report about its own import.
//
// unapplied are custom failover groups it could not build at all; partial are
// groups it built with fewer entries than the primary sent, so it fails over across
// fewer providers for them; unappliedModels are per-model disables it has no model
// to apply. All three name their subjects rather than only counting them, because
// the names point at the routing that is missing, thinner, or unmatched. Unapplied
// is the most severe case, and carries the count as well.
//
// With none, the divergence is one Front Desk measured but the member did not
// explain: it committed the config, reported nothing wrong, and still does not
// match. A count there would read "could not build 0 failover group(s)".
func divergenceMessage(member string, unapplied, partial, unappliedModels []string) string {
	var clauses []string
	if len(unapplied) > 0 {
		clauses = append(clauses, fmt.Sprintf("could not build %d failover group(s): %s",
			len(unapplied), joinCapped(unapplied, divergenceMessageMaxNames)))
	}
	if len(partial) > 0 {
		clauses = append(clauses, fmt.Sprintf("built %s with fewer entries than the primary has",
			joinCapped(partial, divergenceMessageMaxNames)))
	}
	if len(unappliedModels) > 0 {
		clauses = append(clauses, fmt.Sprintf("does not hold %s, which the primary has switched off",
			joinCapped(unappliedModels, divergenceMessageMaxNames)))
	}
	if len(clauses) == 0 {
		return fmt.Sprintf("%s applied the config but does not match the primary's config", member)
	}
	return fmt.Sprintf("%s applied the config but %s", member, strings.Join(clauses, ", and "))
}

// unmeasuredMessage renders the operator-facing line for a member whose config
// hash cannot be read. It says unknown rather than wrong, because that is what
// Front Desk knows, and carries the read failure so the operator can tell an
// unreachable endpoint from a member too old to serve it.
func unmeasuredMessage(member, cause string) string {
	if cause == "" {
		return fmt.Sprintf("%s cannot be measured: Front Desk cannot read its config hash, so whether it holds the primary's config is unknown", member)
	}
	return fmt.Sprintf("%s cannot be measured: Front Desk cannot read its config hash (%s), so whether it holds the primary's config is unknown", member, cause)
}

// clearMemberIncomplete forgets everything about a member whose hash now matches
// the primary's and emits config.sync_recovered once on the transition out, so a
// later divergence re-emits config.sync_incomplete. A member that was never
// flagged emits nothing, keeping ordinary successful passes quiet. Dropping the
// whole entry is deliberate: a converged member has no retry to bound and no
// divergence to describe.
//
// The only way out of the flagged state, and it runs from the auto-sync loop
// alone. A manual sync does not clear the flag: its only evidence is the member's
// own report of its import, which is the trust this criterion replaces. With
// auto-sync off a flagged member keeps its amber badge however many times the
// wizard runs, until a pass measures it as matching.
func (s *Server) clearMemberIncomplete(ctx context.Context, m *Member) {
	s.syncIncompleteMu.Lock()
	was := s.syncIncomplete[m.ID].diverged
	delete(s.syncIncomplete, m.ID)
	s.syncIncompleteMu.Unlock()
	if !was {
		return
	}
	s.emit(ctx, Event{
		Type: "config.sync_recovered", Severity: "success", Source: "frontdesk",
		Message: fmt.Sprintf("%s now holds the primary's config", m.Name), MemberID: m.ID,
	})
}

// incompleteSnapshot copies the diverged set under its lock for the fleet state
// calculation, mirroring heldSnapshot. Members merely pushed to are not in it:
// only a measured divergence degrades the fleet. Disabling auto-sync freezes the
// set rather than clearing it, so the fleet does not report a false ok.
func (s *Server) incompleteSnapshot() map[string]bool {
	s.syncIncompleteMu.Lock()
	defer s.syncIncompleteMu.Unlock()
	out := make(map[string]bool, len(s.syncIncomplete))
	for id, st := range s.syncIncomplete {
		if st.diverged {
			out[id] = true
		}
	}
	return out
}

// shouldSkipIncompleteRetry reports whether a member's re-push is still
// rate-limited. The caller keeps the member not-converged either way; this only
// suppresses the import.
func (s *Server) shouldSkipIncompleteRetry(memberID string, now time.Time) bool {
	s.syncIncompleteMu.Lock()
	defer s.syncIncompleteMu.Unlock()
	st, ok := s.syncIncomplete[memberID]
	if !ok || st.lastAttempt.IsZero() {
		return false
	}
	return now.Sub(st.lastAttempt) < incompleteRetryInterval
}

// resetIncompleteRetries drops every retry timer so each member is pushed again on
// the next pass. The entries stay, so the badge and the alert are unaffected.
// Called when the operator's intent changes (the primary's config moved, or
// auto-sync was enabled), where waiting out the interval would delay a deliberate
// edit. It also clears the "has had its chance" signal, so the pass right after
// such a change flags nobody.
func (s *Server) resetIncompleteRetries() {
	s.syncIncompleteMu.Lock()
	defer s.syncIncompleteMu.Unlock()
	for id, st := range s.syncIncomplete {
		st.lastAttempt = time.Time{}
		s.syncIncomplete[id] = st
	}
}
