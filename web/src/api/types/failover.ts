export interface FailoverEntry {
	model_uuid: string;
	model_id: string;
	provider_id: string;
	provider_name: string;
	display_name: string;
	enabled: boolean;
	model_enabled: boolean;
	provider_enabled: boolean;
	/** True when a user disabled the model; false when discovery auto-disabled it
	 * (model no longer offered by the provider). Drives the N/A reason tooltip. */
	disabled_manually: boolean;
	context_length: number | null;
	owned_by: string;
}
export interface FailoverGroup {
	id: string;
	display_model: string;
	display_name: string | null;
	description: string;
	group_enabled: boolean;
	auto_created: boolean;
	entries: FailoverEntry[];
	total_tokens: number;
	created_at: string;
	updated_at: string;
}
export interface FailoverListResponse {
	groups: FailoverGroup[];
	last_synced_at: string | null;
}
export interface CreateFailoverGroupRequest {
	display_model: string;
	display_name?: string;
	description?: string;
	entry_ids: string[];
}
export interface UpdateFailoverGroupRequest {
	display_model?: string;
	display_name?: string;
	description?: string;
	group_enabled?: boolean;
	priority_order?: string[];
	entry_enabled?: Record<string, boolean>;
}
export interface CircuitBreakerStatus {
	closed: number;
	half_open: number;
	open: number;
	providers?: CircuitBreakerProviderStatus[];
}
export interface CircuitBreakerProviderStatus {
	provider_id: string;
	provider_name?: string;
	state: "closed" | "open" | "half-open";
	consecutive_fails: number;
	opened_at?: string;
	cooldown_ms?: number;
	next_retry_at?: string;
	// True when the cooldown currently governing this circuit was pinned to the
	// provider's quota reset deadline rather than the ordinary retry backoff. It
	// describes the override in force, not a claim that traffic is blocked right
	// now; when set, next_retry_at is that reset deadline.
	quota_pinned?: boolean;
	// True when a probe backoff is in force on this circuit: the ordinary
	// cooldown doubled once per half-open probe that failed since the circuit
	// last closed, up to circuit_breaker_backoff_max. Like quota_pinned it
	// describes an override in force, not whether traffic is blocked right now.
	// Both can be set at once; cooldown_ms and next_retry_at are then whichever
	// reaches further, which is nearly always the pin (a pin is floored at the
	// backoff when it is stamped). Omitted when false.
	backed_off?: boolean;
	// How many half-open probes have failed since the circuit last closed. Sent
	// whenever it is non-zero, even with backoff switched off: it is what
	// happened, where backed_off is what governs.
	failed_probes?: number;
	// The derived provider-wide verdict: whether the breaker is skipping this
	// provider for every model. Circuits are keyed (provider, resolved upstream
	// model), so `state` above describes the provider's most degraded circuit and
	// the two legitimately disagree: one open model at the default span of 2
	// gives state "open" with provider_open false, a provider still serving
	// everything else. Always sent, false included, so a consumer never has to
	// re-derive it from open_models and a span setting it cannot see.
	provider_open: boolean;
	// The resolved upstream model ids the breaker is currently blocking, sorted.
	// Omitted when empty, so absent means none. Exactly the set provider_open is
	// counted from; circuits owed a probe are not in it, because they block
	// nothing.
	open_models?: string[];
	// Every circuit the row above is built from, sorted by model, each with its
	// own state, wait and last verdict. Additive: open_models stays what
	// entryCircuitStatus keys on, and a member from before this field simply
	// omits it, so a consumer must fall back to open_models when it is absent.
	circuits?: CircuitStatus[];
}
// One (provider, resolved upstream model) circuit as the detail endpoint
// reports it. The row-level fields above describe the provider's most
// degraded circuit; these are the same fields at the level the breaker keeps
// them, plus the verdict that last landed on the circuit.
export interface CircuitStatus {
	model: string;
	state: "closed" | "open" | "half-open";
	consecutive_fails: number;
	opened_at?: string;
	cooldown_ms?: number;
	next_retry_at?: string;
	// The overrides governing THIS circuit's cooldown, unlike the row's
	// quota_pinned, which is the verdict's "any blocking circuit is pinned" arm.
	quota_pinned?: boolean;
	pin_source?: "advisor" | "response";
	backed_off?: boolean;
	failed_probes?: number;
	// Why the circuit was last charged, credited, pinned or released ("upstream
	// status 429 (saturated)", "success", "quota pin retargeted (advisor)"),
	// the upstream status behind it (omitted when none was seen) and when. For
	// an open circuit this is why it opened.
	last_cause?: string;
	last_status?: number;
	last_at?: string;
}
// Outcome of an operator forcing one provider's circuit back closed.
// previous_state is what the breaker reported a moment before the reset, and
// reset is false when there was nothing to clear (an already-closed or
// never-tracked provider), so the UI can report a no-op honestly instead of
// claiming a recovery that did not happen.
export interface CircuitBreakerResetResult {
	provider_id: string;
	previous_state: "closed" | "open" | "half-open";
	reset: boolean;
	// The upstream model id when the reset was scoped with ?model=. API-only
	// today: the dashboard's per-entry reset button clears the whole provider.
	model?: string;
}
export interface DeletedGroupInfo {
	display_model: string;
	reason: string;
	provider_count: number;
	provider_names: string[];
}
export interface PrunedEntryInfo {
	group_display_model: string;
	pruned_model_ids: string[];
}
export interface UpdatedGroupInfo {
	display_model: string;
	removed_model_ids?: string[];
	added_model_ids?: string[];
}
export interface DisabledGroupInfo {
	display_model: string;
	effective_count: number;
	reason: string;
}
export interface SyncResult {
	deleted_groups: DeletedGroupInfo[];
	updated_groups?: UpdatedGroupInfo[];
	disabled_groups?: DisabledGroupInfo[];
	purged_entries?: PrunedEntryInfo[];
}
