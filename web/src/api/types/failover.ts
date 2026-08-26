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
