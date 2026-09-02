package frontdesk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// The Front Desk side of HA fleet config sync: it drives the member-side
// /api/config/export and /api/config/import endpoints, pulling the chosen
// primary's config and pushing it to every other member so the fleet converges
// on one configuration.
//
// A config replace can remove providers and keys on a replica, so it is a
// deliberate, primary-driven, double-confirmed action. No key material reaches
// the browser or the logs; only names and counts.

const (
	memberConfigExportPath = "/api/config/export"
	memberConfigImportPath = "/api/config/import"

	// fleetSourceGenHeader carries the monotonic source generation (auto_sync_gen)
	// on a real import so the member's commit fence can refuse an out-of-order
	// stale push. It must match internal/api.fleetSourceGenHeader; a member that
	// does not know the header ignores it.
	fleetSourceGenHeader = "X-Fleet-Source-Gen"
)

// manualSyncReason is stamped on a member's last-config-sync marker and audit
// event when an operator drives the sync rather than the automatic loop. It
// names who triggered it, a paired device or the dashboard, so the Members
// table and event log attribute the run.
func manualSyncReason(actor string) string {
	return "manual sync by " + actor
}

// syncResultItem is one member's outcome from a fleet sync action.
type syncResultItem struct {
	MemberID string `json:"member_id"`
	Name     string `json:"name"`
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
	// Incomplete marks a member that applied the config without materialising
	// all of it. Distinct from Error alone, which also covers pushes the member
	// rejected outright.
	Incomplete bool     `json:"incomplete,omitempty"`
	Unapplied  []string `json:"unapplied,omitempty"`
	// Partial names the custom failover groups the member built with fewer
	// entries than the primary sent. It rides alongside a successful result: the
	// member applied everything it was asked to, it just holds fewer models.
	Partial []string `json:"partial,omitempty"`
	// UnappliedModels names per-model disables the member could not apply because
	// it holds no such model, as provider/model_id. Rides alongside a successful
	// result for the same reason as Partial.
	UnappliedModels []string `json:"unapplied_models,omitempty"`
	// TimedOut marks a push whose deadline expired before the member answered.
	// The member may have committed the config and simply taken longer than
	// memberSyncTimeout to say so, which is what a long member-side discovery
	// looks like from here. Not on the wire: it only tells the auto-sync loop the
	// member did receive the config, so the re-push is rate-limited.
	TimedOut bool `json:"-"`
	// Unconfirmed marks a push whose answer was lost in either of the two ways a
	// still-running import looks like a failure from here: the deadline expired
	// (TimedOut), or a 5xx came back that can stand in front of a live import
	// (lostAnswer5xx). The import may have landed, so the auto-sync loop
	// rate-limits the re-push instead of restarting the member's import every
	// tick. Not on the wire.
	Unconfirmed bool `json:"-"`
}

// memberImportResult mirrors internal/api.importResponse so Front Desk can read
// a member's import/dry-run outcome.
type memberImportResult struct {
	SchemaVersionOK bool `json:"schema_version_ok"`
	MasterKeyOK     bool `json:"master_key_ok"`
	Applied         bool `json:"applied"`
	// Stale is true when the member's commit fence refused this import because a
	// newer source generation already applied (a rearm/repoint superseded this
	// push). It is a benign, expected outcome, not a sync failure.
	Stale bool `json:"stale,omitempty"`
	// Incomplete is true when the member committed the config but could not
	// materialise all of it. Absent on a member too old to report it, which
	// decodes to false and reads as complete.
	Incomplete bool `json:"incomplete,omitempty"`
	// Unapplied names the custom failover groups the member did not build.
	Unapplied []string `json:"unapplied,omitempty"`
	// Partial names the custom failover groups the member built with fewer entries
	// than the primary sent, because it holds fewer of their models. Travels with
	// Incomplete = false: the member applied everything it was asked to.
	Partial []string `json:"partial,omitempty"`
	// UnappliedModels names per-model disables the member could not apply because
	// it holds no such model. Same disposition as Partial: reported, not a failure.
	UnappliedModels []string `json:"unapplied_models,omitempty"`
	// ModelStateFailed is true when the member's per-model disable reconcile failed
	// outright, so it is still routing to models the primary switched off. It is
	// the other fault Incomplete can mean, and the one with no names to report.
	// Absent on a member too old to report it, which reads as false.
	ModelStateFailed bool             `json:"model_state_failed,omitempty"`
	Diff             memberConfigDiff `json:"diff"`
}

type memberEntityDiff struct {
	Added   []string `json:"added"`
	Updated []string `json:"updated"`
	Removed []string `json:"removed"`
}

type memberConfigDiff struct {
	Providers      memberEntityDiff `json:"providers"`
	VirtualKeys    memberEntityDiff `json:"virtual_keys"`
	Settings       memberEntityDiff `json:"settings"`
	FailoverGroups memberEntityDiff `json:"failover_groups"`
	Users          memberEntityDiff `json:"users"`
}

// counts totals the diff across all entity kinds.
func (d memberConfigDiff) counts() (added, updated, removed int) {
	for _, e := range []memberEntityDiff{d.Providers, d.VirtualKeys, d.Settings, d.FailoverGroups, d.Users} {
		added += len(e.Added)
		updated += len(e.Updated)
		removed += len(e.Removed)
	}
	return added, updated, removed
}

// prepareMemberSync runs the dry-run that gates the wizard's destructive replace
// and reports whether the caller should proceed to applyMemberConfig. It returns
// (item, proceed):
//
//   - proceed=true (item nil): the member is reachable, syncable and changing, or
//     it is unreachable / version- or MASTER_KEY-blocked. Either way the caller
//     runs applyMemberConfig, whose import is authoritative and reports the real
//     cause; a blocked or unreachable member cannot be written destructively.
//   - proceed=false (item set): the member is already converged, reported OK with
//     no import. The caller skips applyMemberConfig and records item as-is.
//
// A member the dry run reports as converged is skipped rather than handed a no-op
// import, which would run member-side model discovery for no reason.
//
// Front Desk takes no snapshot before overwriting a member; members back
// themselves up on their own schedule, so a bad propagation cannot be rolled
// back from here.
func (s *Server) prepareMemberSync(ctx context.Context, m *Member, token string, export []byte) (*syncResultItem, bool) {
	preview, status, err := s.pushMemberImport(ctx, m, token, export, true, 0) // dry-run: gen unused (no fence header)
	if err != nil || status != http.StatusOK || !preview.SchemaVersionOK || !preview.MasterKeyOK {
		return nil, true // unreachable or blocked: let applyMemberConfig report the real cause
	}
	if added, updated, removed := preview.Diff.counts(); added+updated+removed == 0 {
		// Already in sync: no import, and no last_config_sync_at stamp, since that
		// column means a real config write. The live "verified in sync" heartbeat does
		// advance, so the Members table shows the wizard confirmed this member.
		s.poller.SetAutoSyncVerified(m.ID, time.Now().UTC())
		return &syncResultItem{MemberID: m.ID, Name: m.Name, OK: true}, false
	}
	return nil, true // this member is changing: proceed to the authoritative import
}

// recordFleetSyncRun stamps the last-run marker when a sync action updated at
// least one member, so the wizard can show it has run before. A persistence
// failure is non-fatal: the sync itself already succeeded, so it is logged and
// swallowed rather than surfaced.
func (s *Server) recordFleetSyncRun(ctx context.Context, primary *Member, results []syncResultItem) {
	changed := false
	for _, r := range results {
		// An incomplete member counts as a config write: it committed the config and
		// only failed to materialise part of it. Excluding it would freeze the fleet
		// marker the staleness watchdog reads while one member stays diverged.
		if r.OK || r.Incomplete {
			changed = true
			break
		}
	}
	if !changed {
		return
	}
	if err := s.store.SetFleetSyncState(ctx, primary.ID, primary.Name, time.Now().UTC()); err != nil {
		debuglog.Warn("frontdesk: record fleet sync state", "error", err)
	}
}

// applyMemberConfig imports the primary's config onto one member and records an
// audit event for the outcome. On success it stamps the member's last-config-sync
// marker with reason (shown in the Members table), so both the wizard and the
// auto-sync loop record why and when a member last converged.
//
// emitSuccessEvent separates the two callers' notion of success. The wizard sets
// it true: an operator drove this sync, so it wants one event per member and the
// heartbeat moves with the write. The auto-syncer sets it false: it emits one
// roll-up and takes its heartbeat from its own hash comparison. Failure events
// fire either way.
func (s *Server) applyMemberConfig(ctx context.Context, m *Member, token string, export []byte, reason string, emitSuccessEvent bool, sourceGen int64, pushedHash string) syncResultItem {
	res := syncResultItem{MemberID: m.ID, Name: m.Name}
	// How long the push ran separates a 5xx that cut a live import from one the
	// answerer had at hand (see lostAnswer5xx).
	pushStart := time.Now()
	out, status, err := s.pushMemberImport(ctx, m, token, export, false, sourceGen)
	// Carried on the success path too: neither a group built short nor a disable
	// the member has no model for is a failure to apply, and the auto-sync loop
	// needs the names for its divergence alert.
	res.Partial = out.Partial
	res.UnappliedModels = out.UnappliedModels
	switch {
	case err != nil && status == 0 && isTimeout(err):
		// The deadline expired with the member still working, so unlike a refusal
		// or an unreachable host this push may have landed. Its own outcome, so the
		// loop rate-limits the re-push instead of re-importing every tick and
		// restarting the member-side discovery.
		res.Error = "this member did not answer in time"
		res.TimedOut = true
		res.Unconfirmed = true
		// No stamp lands below, but the import may still complete member-side; the
		// pass that measures the member holding this exact config stamps it then.
		s.markUnconfirmedPush(m.ID, pushedHash)
	case err != nil && status == 0:
		res.Error = "could not reach this member"
	case err != nil:
		// The member answered, with a status we cannot apply: surface it so a wrong
		// stored token or a member-side error is not mislabeled "offline", and
		// carry the member's own reason when it gave one.
		res.Error = fmt.Sprintf("this member rejected the request (HTTP %d)", status)
		var refusal *memberRefusal
		if errors.As(err, &refusal) && refusal.reason != "" {
			res.Error = fmt.Sprintf("this member rejected the request (HTTP %d): %s", status, refusal.reason)
		}
		if lostAnswer5xx(status, time.Since(pushStart)) {
			// This 5xx is not proof the import failed: a reverse proxy between Front
			// Desk and the member answers 502/504 when the import outlives its own
			// read timeout, with the member still applying behind it. The hash
			// binding covers the other slow 5xx source, an import that errored
			// before commit: nothing landed there, so the member can only reach this
			// exact hash if an operator restores precisely this config.
			res.Error = fmt.Sprintf("this member's answer was lost (HTTP %d); it may still be applying", status)
			res.Unconfirmed = true
			s.markUnconfirmedPush(m.ID, pushedHash)
		}
	case !out.SchemaVersionOK:
		// Schema is checked before MASTER_KEY: a 422 short-circuits before the
		// decrypt canary, leaving master_key_ok an unevaluated false.
		res.Error = "version mismatch with the primary"
	case !out.MasterKeyOK:
		res.Error = "MASTER_KEY does not match the primary"
	case out.Stale:
		// The member's commit fence refused this push because a newer source
		// generation already applied, the benign outcome of a rearm or repoint
		// landing mid-flight. The superseding pass is authoritative, so this one
		// does not stamp last-sync, does not count as converged, and emits no
		// failure event; res.OK stays false so the caller leaves the member to the
		// newer pass.
		res.Error = "superseded by a newer sync"
		debuglog.Debug("frontdesk: config sync superseded by a newer generation", "member", m.Name, "source_gen", sourceGen)
		// Its own label: a fence supersede is benign, and folding it into "err"
		// would make routine rearms look like sync failures on a graph.
		recordConfigSync("superseded")
		return res
	case !out.Applied:
		res.Error = "this member did not apply the config"
	case out.Incomplete:
		// The member committed the config but could not build part of it, so this
		// push did not apply it and res.OK stays false. The auto-sync loop decides
		// convergence from the hash instead; what this arm adds is the names that
		// make its alert specific.
		res.Error = incompleteMessage(out.Unapplied, out.ModelStateFailed)
		res.Incomplete = true
		res.Unapplied = out.Unapplied
		recordConfigSync("incomplete")
		// The member did commit this config, so its last-sync marker advances even
		// though res.OK stays false. Otherwise a member that can never build one
		// group shows "Last Config Sync" frozen at its last clean apply, and the
		// staleness watchdog raises config.autosync_stale on top of the divergence
		// alert already naming the real problem. A write failure is logged and
		// dropped: the member is reported diverged either way.
		if err := s.store.SetMemberLastSync(ctx, m.ID, time.Now().UTC(), reason); err != nil {
			debuglog.Warn("frontdesk: stamp member last-sync", "member", m.Name, "error", err)
		} else {
			// This stamp covers a lost-answer push of this same config; a concurrent
			// push of newer config keeps its own flag (see clearUnconfirmedPush).
			s.clearUnconfirmedPush(m.ID, pushedHash)
		}
		// This arm returns before the shared failure branch below, so without this an
		// incomplete apply would leave no trace in the logs when alerting is off.
		debuglog.Warn("frontdesk: config sync incomplete", "member", m.Name, "error", res.Error)
		return res
	default:
		res.OK = true
	}

	// A failed last-sync write fails the whole result: the store and UI would still
	// show the member unsynced, so it must not be marked converged, and neither the
	// success event nor the verified heartbeat may fire.
	if res.OK {
		if err := s.store.SetMemberLastSync(ctx, m.ID, time.Now().UTC(), reason); err != nil {
			debuglog.Warn("frontdesk: stamp member last-sync", "member", m.Name, "error", err)
			res.OK = false
			res.Error = "applied but could not record the sync stamp"
		} else {
			// This stamp covers a lost-answer push of this same config; a concurrent
			// push of newer config keeps its own flag (see clearUnconfirmedPush).
			s.clearUnconfirmedPush(m.ID, pushedHash)
		}
	}

	if res.OK {
		recordConfigSync("ok")
		if emitSuccessEvent {
			// The wizard's path: an operator drove this sync, so a completed write is
			// what they asked to be told about. The auto-sync loop passes false and
			// takes no stamp here: a successful write proves the member took the
			// config, not that it ended up holding it.
			s.poller.SetAutoSyncVerified(m.ID, time.Now().UTC())
			s.emit(ctx, Event{
				Type: "config.synced", Severity: "info", Source: "frontdesk",
				Message: fmt.Sprintf("Config synced to %s", m.Name), MemberID: m.ID,
				// reason carries who and why (e.g. "manual sync by Pixel (operator)"
				// or "did not hold the primary's config") for the event log.
				Metadata: map[string]any{"reason": reason},
			})
		}
	} else {
		recordConfigSync("err")
		debuglog.Warn("frontdesk: config sync failed", "member", m.Name, "error", res.Error)
		// An unconfirmed push (timed out, or 5xx'd in a way that can stand in front
		// of a live import) is published at info, not warning: alert dispatch takes
		// its notification severity from the live event, and paging an operator for
		// a condition this message calls probably fine is noise. The type stays the
		// same, since the push still did not converge the member. A genuine
		// refusal, an unreachable member, or a mismatch still warns.
		severity := "warning"
		if res.Unconfirmed {
			severity = "info"
		}
		s.emit(ctx, Event{
			Type: "config.sync_failed", Severity: severity, Source: "frontdesk",
			Message: syncFailureMessage(m.Name, res.Error, res.TimedOut), MemberID: m.ID,
			// error carries the specific cause the message renders; timed_out and
			// unconfirmed are always present so a consumer reads one shape rather
			// than testing for the keys. unconfirmed separates a lost answer, which
			// is rate-limited and may prove itself landed on a later verification
			// pass, from a genuine rejection.
			Metadata: map[string]any{"reason": reason, "error": res.Error, "timed_out": res.TimedOut, "unconfirmed": res.Unconfirmed},
		})
	}
	return res
}

// incompleteMessage renders what a member committed but could not materialise.
//
// Incomplete covers two faults, and they send the operator to different places:
// a custom failover group that would not build (those models serve 404 for
// hotel/<group>), and a per-model disable reconcile that failed (the member is
// still routing to models the primary switched off). Only the first has names,
// so each fault gets its own clause.
func incompleteMessage(unapplied []string, modelStateFailed bool) string {
	var clauses []string
	if len(unapplied) > 0 {
		clauses = append(clauses, fmt.Sprintf("%d failover group(s) could not be built here: %s",
			len(unapplied), strings.Join(unapplied, ", ")))
	}
	if modelStateFailed {
		clauses = append(clauses, "this member could not apply the primary's per-model settings")
	}
	if len(clauses) == 0 {
		// Neither named: a member reporting incomplete for a fault this build has no
		// field for, or a group-build transaction that failed before any group was
		// evaluated.
		return "applied, but this member could not materialise all of it"
	}
	return "applied, but " + strings.Join(clauses, ", and ")
}

// syncFailureMessage renders the operator-facing line for a push that did not
// converge the member, naming the specific cause: they range from an unreachable
// host to a MASTER_KEY mismatch, and each points at different work.
//
// A timed-out push gets its own wording because it is not a refusal: the member
// took the request and is likely still importing, so calling it a failure sends
// the operator after a member that is working.
func syncFailureMessage(member, cause string, timedOut bool) string {
	if timedOut {
		return fmt.Sprintf("%s did not answer the config push in time; it may still be applying", member)
	}
	if cause == "" {
		return fmt.Sprintf("Failed to sync config to %s", member)
	}
	return fmt.Sprintf("Failed to sync config to %s: %s", member, cause)
}

// isTimeout reports whether a member call failed because its deadline expired,
// as opposed to being refused, unreachable, or cancelled. Both shapes count: the
// client's own Timeout, and an expired context deadline. A cancelled context is
// deliberately not a timeout, so a pass aborted by a rearm is never mistaken for
// a slow member.
func isTimeout(err error) bool {
	var nerr net.Error
	if errors.As(err, &nerr) && nerr.Timeout() {
		return true
	}
	return errors.Is(err, context.DeadlineExceeded)
}

// unconfirmed5xxFloor is how long a real import call must have run for a 5xx
// other than 504 to be read as a proxy giving up on a live import rather than a
// refusal the answerer already had at hand. A reverse proxy only 5xxes a live
// import once its own read timeout expires, 30-60s at the low end, while a
// connection-refused 502 (the member's process is down) or an immediate
// member-side 500 answers within milliseconds.
const unconfirmed5xxFloor = 10 * time.Second

// lostAnswer5xx reports whether a 5xx answer to a real import may be standing
// in front of an import that is still running member-side, making it a lost
// answer rather than a refusal. A 4xx is a definite refusal (auth, schema) and
// never qualifies.
//
// 504 qualifies on its own: a gateway timeout means an intermediary waited for
// the member and gave up, however long that wait was configured to be. Any
// other 5xx qualifies only after unconfirmed5xxFloor. An instant 502 or 500
// cannot mean "still importing": it is a proxy whose upstream is down, or a
// member handler erroring on the spot, and treating it as maybe-landed would
// rate-limit the re-push of a push that demonstrably did nothing.
func lostAnswer5xx(status int, elapsed time.Duration) bool {
	if status == http.StatusGatewayTimeout {
		return true
	}
	return status >= http.StatusInternalServerError && elapsed >= unconfirmed5xxFloor
}

// maxMemberConfigExportBody is the read limit for a member's config envelope. It
// matches the member-side import cap (internal/api.maxConfigImportBody), because
// this body is only ever re-posted to that endpoint: reading less than a member
// would accept refuses an envelope the fleet could have synced, and reading more
// buys nothing since the receiving member would reject it.
//
// It is its own limit rather than the shared maxMemberRespBody, which is eight
// times smaller and sized for fixed-shape documents. An envelope grows with the
// fleet's providers, virtual keys, users and groups, and a refused read aborts
// the whole pass, so no member converges.
const maxMemberConfigExportBody = 8 << 20

// fetchMemberExport reads a member's config envelope as raw JSON so it can be
// re-posted to replicas verbatim (preserving the base64 key ciphertext).
func (s *Server) fetchMemberExport(ctx context.Context, m *Member, token string) ([]byte, error) {
	status, body, err := s.callMemberLimited(ctx, s.probe, maxMemberConfigExportBody,
		http.MethodGet, m.URL, memberConfigExportPath, token, nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("member config-export returned %d", status)
	}
	return body, nil
}

// pushMemberImport posts the config envelope to a member. dryRun=true asks for a
// diff without writing. A 409 (MASTER_KEY mismatch) or 422 (schema) is parsed
// into the result rather than treated as a transport error, so the caller can
// surface a precise disposition. The returned status is the member's HTTP status
// (0 on a transport failure where the member never answered), so the caller can
// tell a genuinely unreachable member from one that answered with a rejecting
// code (e.g. 401/403 wrong token, 500) and report the real cause.
func (s *Server) pushMemberImport(ctx context.Context, m *Member, token string, export []byte, dryRun bool, sourceGen int64) (memberImportResult, int, error) {
	path := memberConfigImportPath
	var headers [][2]string
	if dryRun {
		path += "?dryRun=1"
		// A dry run is read-only and never fenced, so it carries no
		// source-generation header.
	} else {
		// Stamp the commit fence on the real import so the member can refuse a
		// stale, out-of-order push (a primary repoint that lands mid-flight).
		headers = append(headers, [2]string{fleetSourceGenHeader, strconv.FormatInt(sourceGen, 10)})
	}
	// The import client has a longer deadline than the health probe: a real
	// import runs model discovery on the member, which routinely exceeds the 4s
	// probe timeout, and timing out there would mislabel a successful import as
	// "could not reach this member".
	status, body, err := s.callMemberWith(ctx, s.syncClient, http.MethodPost, m.URL, path, token, strings.NewReader(string(export)), headers...)
	if err != nil {
		return memberImportResult{}, 0, err
	}
	switch status {
	case http.StatusOK, http.StatusConflict, http.StatusUnprocessableEntity:
		var res memberImportResult
		if err := json.Unmarshal(body, &res); err != nil {
			return memberImportResult{}, status, errors.New("frontdesk: parse member import response")
		}
		return res, status, nil
	default:
		return memberImportResult{}, status, &memberRefusal{status: status, reason: refusalReason(body)}
	}
}

// memberRefusal is a member answering the import with a status Front Desk
// cannot apply, carrying whatever reason the member gave. A member refuses an
// envelope it will not write (a provider field its own admin API would reject,
// a wrong token) with a 400 whose body names the field, so the operator driving
// the fleet sees the reason and not just the status.
type memberRefusal struct {
	status int
	reason string
}

func (e *memberRefusal) Error() string {
	if e.reason == "" {
		return fmt.Sprintf("member config-import returned %d", e.status)
	}
	return fmt.Sprintf("member config-import returned %d: %s", e.status, e.reason)
}

// maxRefusalReasonRunes bounds the member's reason as shown to the operator,
// in runes: a refusal names one field and one value, and a body past this is
// not a reason but a page. The member's own log carries the full text.
const maxRefusalReasonRunes = 240

// refusalReason extracts the operator-readable reason from a member's
// refusal body: the "error" member of a JSON body (the member's coded
// errors), else the first line of a plain-text one (http.Error). An HTML
// page (a reverse proxy's or the SPA's 404) carries no reason.
//
// The text is member-authored and reaches the dashboard, the event log, a
// paired monitor device and the alert push, so it is reduced to one line
// (Unicode line separators included), stripped of control and format
// characters (bidi overrides, zero-width joiners), stripped of any userinfo
// in a URL it quotes (a refused setting or base_url is echoed raw by
// url.Parse, password included), key-shape masked, and bounded in runes.
// The exact-credential layer runs too but has nothing to match here: Front
// Desk decrypts no provider key, so its held set is empty and the shape layer
// does the work in this process.
func refusalReason(body []byte) string {
	text := strings.TrimSpace(string(body))
	if text == "" || strings.HasPrefix(text, "<") {
		return ""
	}
	if text[0] == '{' {
		var coded struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &coded) != nil || coded.Error == "" {
			return ""
		}
		text = coded.Error
	}
	if i := strings.IndexFunc(text, isLineBreak); i >= 0 {
		text = text[:i]
	}
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, text)
	text = urlUserinfoRE.ReplaceAllString(text, "$1")
	// The byte bound here is generous on purpose (four bytes per rune is the
	// UTF-8 maximum): the rune bound below is the one the operator sees.
	text = strings.TrimSpace(util.MaskCredentialsBounded(nil, text, 4*maxRefusalReasonRunes))
	if runes := []rune(text); len(runes) > maxRefusalReasonRunes {
		text = strings.TrimSpace(string(runes[:maxRefusalReasonRunes])) + "…"
	}
	return text
}

// isLineBreak is every rune a terminal or a browser treats as a line break:
// CR, LF, NEL and the Unicode line and paragraph separators.
func isLineBreak(r rune) bool {
	return r == '\r' || r == '\n' || r == 0x85 || r == 0x2028 || r == 0x2029
}
