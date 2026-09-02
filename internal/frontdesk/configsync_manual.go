package frontdesk

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// The operator-driven half of fleet config sync: the POST /api/config/sync
// handler, the detached run it parks on, and the run's result shape. The member
// plumbing this and the automatic loop share (export, dry-run, import, the
// per-member result) lives in configsync.go.

// errPrimaryExportUnreadable stops a run before any member is touched: the
// primary's config could not be read, so there is nothing to push. A sentinel
// because the run happens away from the handler that renders it, and this is
// the one failure answered as a 502 rather than as a store error.
var errPrimaryExportUnreadable = errors.New("could not read the primary's config")

// The two refusals of the designation guard in runConfigSync. Sentinels so the
// handler can map them to a coded 409 while the guard itself lives beside the
// writes it protects, on the detached context, where it is re-checked before
// every push.
var (
	errPrimaryNotDesignated = errors.New("designate a fleet primary before syncing")
	errSyncSourceNotPrimary = errors.New("the sync source must be the designated fleet primary; change the primary first (admin token required)")
)

// Stable codes for the designation guard's answers. Coded rather than bare
// text because Bellhop's error reader only surfaces the message from a coded
// envelope, and a bare 403 there reads as "this device is a monitor" and hides
// the operator controls. 409 is the honest status: the request conflicts with
// the fleet's designated state, and the remedy is to change that state first,
// not to hold a different credential.
const (
	primaryNotDesignatedCode = "primary_not_designated"
	syncSourceNotPrimaryCode = "sync_source_not_primary"
	primaryRepointedCode     = "primary_repointed"
)

// syncInterruptedCode is the stable code on the answer to a run shutdown cut
// short, so a client can route on it instead of matching the English message.
const syncInterruptedCode = "sync_interrupted"

// configSyncRun is one manual sync run's outcome, handed back to the handler
// parked on it: the per-member results, or the error that ended the run before
// any member was touched.
type configSyncRun struct {
	primaryID string
	results   []syncResultItem
	err       error
	// interrupted marks a run the server's shutdown ended before it finished, so
	// results covers only part of the fleet. It is answered as a 503 rather than a
	// 200: the wizard reads a 200 as the whole run and would report "N of N synced"
	// for a fleet whose remaining members were never touched.
	interrupted bool
	// notAttempted counts the syncable members the run never reached. Zero on an
	// interrupted run whose last member was the one cut off mid-push.
	notAttempted int
	// repointed marks a run the designation guard stopped: the fleet primary was
	// changed (or cleared) after admission, so the export in hand is no longer
	// the designated one and nothing more may be pushed from it. Answered as a
	// coded 409 with the partial results, never a 200.
	repointed bool
}

// repointedMessage names what the operator is looking at when the designation
// guard stopped a run: a partial run, how much of the fleet it never reached,
// and what to do about it. It reaches the UI as the error text of the 409, so
// it is written for a person.
func (run configSyncRun) repointedMessage() string {
	msg := "the fleet primary was changed while this sync was running, so it stopped"
	if run.notAttempted > 0 {
		msg += fmt.Sprintf("; %d member(s) were not attempted", run.notAttempted)
	}
	return msg + ". Run the sync again from the new primary."
}

// interruptedMessage is the same line for a run cut short by shutdown, carried as
// the error text of the 503.
func (run configSyncRun) interruptedMessage() string {
	msg := "front desk began shutting down during the sync, so it did not finish"
	if run.notAttempted > 0 {
		msg += fmt.Sprintf("; %d member(s) were not attempted", run.notAttempted)
	}
	return msg + ". Run the sync again once front desk is back."
}

// syncableCount counts the members a run still owes a push: the primary is the
// source and a token-less member is skipped without a result, so neither is
// something the operator is waiting on.
func syncableCount(members []*Member, primaryID string) int {
	n := 0
	for _, m := range members {
		if m.ID != primaryID && m.HasToken {
			n++
		}
	}
	return n
}

// configSync pulls the primary's config and applies it to every other member
// Front Desk can authenticate to. Each member is independent: a failure leaves
// that member untouched and is reported.
//
// The run itself happens on a goroutine registered with the server's background
// group, and the handler parks on it so the operator still gets the per-member
// results on this request. Detaching the run from the request context is what
// keeps a client with a short HTTP timeout (Bellhop, a proxy) from cancelling
// member imports, event emits and sync stamps half-way; registering it is what
// keeps that same detached run from outliving Shutdown's drain and recording
// itself against a closed store.
func (s *Server) configSync(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PrimaryID string `json:"primary_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.PrimaryID == "" {
		// Its own arm, ahead of the designation guard in the run: an omitted
		// field would otherwise read as "not the designated source" and tell the
		// caller to confirm an admin token for a change nobody asked for.
		writeCodedError(w, http.StatusBadRequest, "primary_required", "primary_id is required")
		return
	}

	var run configSyncRun
	done := make(chan struct{})
	if !s.StartBackground(s.detachedContext(r), func(ctx context.Context) {
		defer close(done)
		run = s.runConfigSync(ctx, req.PrimaryID)
	}) {
		// Shutdown has begun, and the run is the response here, so say so: a
		// fleet-wide write started now would be cancelled a moment later with
		// nowhere to record what it did.
		http.Error(w, "front desk is shutting down; run the sync again once it is back", http.StatusServiceUnavailable)
		return
	}
	// Bounded by the run itself: every member call carries the sync client's
	// deadline, and the server's lifetime ends the whole run at shutdown, which is
	// the same goroutine the drain is waiting on.
	<-done

	switch {
	case errors.Is(run.err, errPrimaryNotDesignated):
		writeCodedError(w, http.StatusConflict, primaryNotDesignatedCode, run.err.Error())
	case errors.Is(run.err, errSyncSourceNotPrimary):
		writeCodedError(w, http.StatusConflict, syncSourceNotPrimaryCode, run.err.Error())
	case errors.Is(run.err, errPrimaryExportUnreadable):
		http.Error(w, run.err.Error(), http.StatusBadGateway)
	case run.err != nil:
		writeError(w, run.err)
	case run.repointed:
		// Same shape as the interrupted answer below, for the same reason.
		writeJSON(w, http.StatusConflict, map[string]any{
			"code": primaryRepointedCode, "error": run.repointedMessage(),
			"primary_id": run.primaryID, "results": run.results,
		})
	case run.interrupted:
		// The coded-error shape (code + error) with the partial results alongside
		// it: a caller that only reads the message still learns the run was cut
		// short and how much of the fleet it never reached, and one that reads the
		// body sees what did happen. Not a 200, which would read as a complete run
		// of a fleet this size.
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"code": syncInterruptedCode, "error": run.interruptedMessage(),
			"primary_id": run.primaryID, "results": run.results,
		})
	default:
		writeJSON(w, http.StatusOK, map[string]any{"primary_id": run.primaryID, "results": run.results})
	}
}

// runConfigSync is the manual sync run: pull the primary's config, push it to
// every other member, record the run. It carries no ResponseWriter because it
// executes on the server's background group rather than on the handler's
// goroutine; the handler renders what it returns.
//
// ctx ends when the server shuts down, so a fleet-wide push stops between
// members rather than continuing into a store that is about to close.
func (s *Server) runConfigSync(ctx context.Context, primaryID string) configSyncRun {
	// Attribute the run to whoever authenticated it (a paired device or the
	// dashboard). The detached context keeps the request's values, so the device is
	// still resolvable off ctx. Stamped on each member and carried in the audit event.
	reason := manualSyncReason(actorFromContext(ctx))

	// The source must be the primary the operator designated. Picking which
	// member's config the fleet copies is an admin-token confirmed decision
	// (putAutoSync -> SetAutoSyncGuarded), and this run reaches the identical
	// outcome by another door: every other member, the designated primary
	// included, is overwritten with the chosen member's config. An unchecked id
	// from the request would let the operator tier roll the whole fleet back to a
	// stale member's config. "Repair the fleet from member X" is two steps:
	// repoint (admin token), then sync.
	cfg, err := s.store.GetAutoSync(ctx)
	if err != nil {
		return configSyncRun{err: err}
	}
	switch {
	case cfg.PrimaryID == "":
		return configSyncRun{err: errPrimaryNotDesignated}
	case primaryID != cfg.PrimaryID:
		debuglog.Warn("frontdesk: refused config sync from an undesignated source", "requested", primaryID, "designated", cfg.PrimaryID)
		return configSyncRun{err: errSyncSourceNotPrimary}
	}
	// gen is the generation the designation above was read WITH (one row, one
	// scan), never a later re-read. It stamps every push so the members' commit
	// fence can refuse a stale one, and the repoint gate below compares against
	// it. A generation read after the guard would let a repoint landing in
	// between stamp this run's pushes with the new generation, and the fence that
	// exists to refuse a stale source would accept them.
	gen := cfg.Gen

	primary, primaryToken, err := s.memberTokenOrErr(ctx, primaryID)
	if err != nil {
		return configSyncRun{err: err}
	}
	export, err := s.fetchMemberExport(ctx, primary, primaryToken)
	if err != nil {
		return configSyncRun{err: errPrimaryExportUnreadable}
	}

	members, err := s.store.ListMembers(ctx)
	if err != nil {
		return configSyncRun{err: err}
	}

	// The primary's config hash keys any lost-answer push so a later verification
	// pass can stamp it (see unconfirmedSync). Best-effort: with the hash
	// unreadable the run proceeds and a lost answer simply stays unstamped, since
	// a stamp that cannot be tied to the pushed config must not be promised.
	pushedHash, _, err := s.fetchMemberConfigVersion(ctx, primary, primaryToken)
	if err != nil {
		debuglog.Debug("frontdesk: config sync: read primary config hash", "member", primary.Name, "error", err)
		pushedHash = ""
	}

	// The guard is held for the whole run, not just at admission: a run can last
	// minutes (memberSyncTimeout per member), and an admin repointing in the
	// meantime has made a decision this run must not undo. Three layers, as in
	// applyAutoSync: a gate at the top of each member, a gate tightest to the
	// mutation after the slow dry-run, and a watcher that cancels an import
	// already in flight the instant a rearm moves the generation. A read error
	// reports "not moved", so a transient DB failure does not abort a valid run.
	moved := func() bool {
		cur, err := s.store.GetAutoSync(ctx)
		return err == nil && (cur.Gen != gen || cur.PrimaryID != primaryID)
	}
	rearmCh := s.rearmChan()
	passCtx, cancel := context.WithCancel(ctx)
	stopWatch := s.startRearmWatch(passCtx, rearmCh, gen, cancel)
	defer stopWatch()

	primaryBuild := s.poller.memberBuildOf(primary.ID)
	results := make([]syncResultItem, 0)
	notAttempted := 0
	repointed := false
	for i, m := range members {
		if ctx.Err() != nil {
			// The server is shutting down. Stop between members rather than push a
			// config whose outcome nothing can record: the store is waiting on this
			// goroutine to close. The members left here are counted, not silently
			// dropped, so the answer can say how much of the fleet this run never
			// reached.
			notAttempted = syncableCount(members[i:], primary.ID)
			debuglog.Info("frontdesk: manual config sync stopped by shutdown",
				"primary", primary.Name, "not_attempted", notAttempted)
			break
		}
		if moved() {
			notAttempted = syncableCount(members[i:], primary.ID)
			repointed = true
			debuglog.Warn("frontdesk: manual config sync stopped, primary repointed mid-run",
				"source", primary.Name, "not_attempted", notAttempted)
			break
		}
		if m.ID == primary.ID || !m.HasToken {
			continue // the source, and token-less members (flagged in preview), are skipped
		}
		token, ok, err := s.store.MemberToken(ctx, m.ID)
		if err != nil || !ok {
			continue
		}
		if buildSkew(primaryBuild, s.poller.memberBuildOf(m.ID)) {
			// This member runs a different build than the primary; pushing could
			// delete settings it legitimately has. Refuse here even though the
			// wizard gates first, so a bypassed UI cannot force a mismatched sync.
			results = append(results, syncResultItem{
				MemberID: m.ID, Name: m.Name,
				Error: "held: member's build differs from the primary's",
			})
			continue
		}
		// Gate the destructive replace on a dry-run, so an already-converged member
		// is reported without an import.
		if item, proceed := s.prepareMemberSync(passCtx, m, token, export); !proceed {
			results = append(results, *item)
			continue
		}
		// Tightest to the mutation: a repoint can land during this member's slow
		// dry-run. The window between here and the member's commit is covered by
		// passCtx, which the watcher cancels.
		if moved() {
			notAttempted = syncableCount(members[i:], primary.ID)
			repointed = true
			debuglog.Warn("frontdesk: manual config sync stopped before import, primary repointed mid-run",
				"source", primary.Name, "member", m.Name, "not_attempted", notAttempted)
			break
		}
		results = append(results, s.applyMemberConfig(passCtx, m, token, export, reason, true, gen, pushedHash))
	}
	// Checked again after the loop, because a run can be cut short without ever
	// reaching the guards above: the member being pushed when shutdown lands (or
	// when the watcher cancels for a repoint) takes the cancellation on its own
	// HTTP call, and the loop then ends of its own accord with nothing left to
	// skip. Either way the run did not finish, so it reports itself as such
	// rather than as a clean sweep of the fleet.
	//
	// The run marker is skipped with it: it is a write like any other, and a
	// cancelled run is not one the fleet's staleness watchdog should count.
	if ctx.Err() != nil {
		return configSyncRun{
			primaryID: primary.ID, results: results,
			interrupted: true, notAttempted: notAttempted,
		}
	}
	if repointed || passCtx.Err() != nil {
		return configSyncRun{
			primaryID: primary.ID, results: results,
			repointed: true, notAttempted: notAttempted,
		}
	}
	s.recordFleetSyncRun(ctx, primary, results)
	return configSyncRun{primaryID: primary.ID, results: results}
}
