import type {
	DeletedGroupInfo,
	DisabledGroupInfo,
	UpdatedGroupInfo,
} from "./failover";

export interface ModelCapabilities {
	streaming?: boolean;
	vision?: boolean;
	video_input?: boolean;
	audio_input?: boolean;
	reasoning?: boolean;
	tool_calling?: boolean;
	parallel_tool_calls?: boolean;
	structured_output?: boolean;
	pdf_upload?: boolean;
}
export interface Model {
	id: string;
	model_id: string;
	name: string;
	description: string;
	display_name: string;
	provider_id: string;
	provider_name: string;
	/** Owning provider's enabled flag; false means the row is parked, not served. */
	provider_enabled: boolean;
	capabilities: string;
	params: string;
	modality: string;
	input_modalities: string;
	output_modalities: string;
	context_length: number | null;
	max_output_tokens: number | null;
	input_price_per_million: number | null;
	input_price_per_million_cache_hit: number | null;
	output_price_per_million: number | null;
	owned_by: string;
	enabled: boolean;
	disabled_manually: boolean;
	price_customized: boolean;
	created_at: string;
	last_seen_at: string;
}
export interface ModelsCursorResponse {
	entries: Model[];
	total: number;
	/** Rows the proxy can serve (model enabled AND provider enabled). */
	enabled_total: number;
	/** Rows parked under a disabled provider: listed, kept, not served. */
	parked_total: number;
	/**
	 * Rows whose own flag is off, parked or not: what "delete disabled"
	 * removes for these filters.
	 */
	disabled_total: number;
	has_before: boolean;
	has_after: boolean;
}
export interface CandidateModel {
	model_uuid: string;
	model_id: string;
	provider_id: string;
	provider_name: string;
	display_name: string;
	context_length: number | null;
	owned_by: string;
}
export interface ModelChange {
	model_id: string;
	/** Machine-readable code: new_model | reappeared | not_listed */
	reason: string;
}
export interface FieldChange {
	/** Machine-readable code: input_price | output_price | input_price_cache | context_length | max_output_tokens */
	field: string;
	/** Previous value as a number; null/undefined means it was unset. */
	old?: number | null;
	/** New value as a number; null/undefined means it is now unset. */
	new?: number | null;
}
export interface ModelUpdate {
	model_id: string;
	changes: FieldChange[];
}
export interface DiscoveryDiff {
	added?: ModelChange[];
	reenabled?: ModelChange[];
	disabled?: ModelChange[];
	updated?: ModelUpdate[];
	failover_deleted_groups?: DeletedGroupInfo[];
	failover_updated_groups?: UpdatedGroupInfo[];
	failover_disabled_groups?: DisabledGroupInfo[];
}
/**
 * One provider's recorded background-discovery diff. Served as the
 * `informational` feed of GET /api/discovery/status and as the rows returned by
 * POST /api/discovery/changes/ack; the GET that used to serve it is gone.
 */
export interface DiscoveryChangeEntry {
	/** Empty when the provider was deleted after the change was recorded. */
	provider_id?: string;
	provider_name: string;
	source: string;
	detected_at: string;
	diff: DiscoveryDiff;
}
/**
 * Response of POST /api/discovery/changes/ack: exactly the rows that call marked
 * seen. `count` is always 0 (the badge is empty once they are acked).
 */
export interface DiscoveryChangesResponse {
	entries: DiscoveryChangeEntry[];
	count: number;
}
/** What discovery currently believes about one model.
 *
 *  `retired` is the odd one out: it did not come from discovery at all. The
 *  proxy disabled the model from live traffic because the provider kept listing
 *  it and refused every request for it. The distinction matters to the operator,
 *  because a retired model is still listed and was seen moments ago, so the
 *  "last seen" wording and the Retest button that fit the other states would
 *  both mislead.
 *
 *  `pinned` is the other odd one out, in the opposite direction: the operator
 *  enabled the model by hand while the provider's listing stopped naming it, so
 *  discovery keeps counting the misses but never disables it. It is shown so a
 *  forgotten pin cannot rot silently, and never counted: it is a decision, not
 *  a problem. */
export type ClaimState = "gone" | "stale" | "suspect" | "retired" | "pinned";
export interface ModelClaim {
	model_id: string;
	state: ClaimState;
	/** When the provider last listed it; for a gone model, when it went missing. */
	last_seen_at: string;
	missing_scans: number;
	/** Membership transitions over the 30-day claim window. */
	flap_window: number;
	/** Membership transitions since the operator last opened the modal. */
	flap_since_review: number;
	/** When the proxy retired it from traffic. Present only on a `retired` claim;
	 *  `last_seen_at` keeps being refreshed for those, so it cannot date them. */
	retired_at?: string;
	/** When the operator last enabled it by hand. Present only on a `pinned`
	 *  claim; `last_seen_at` dates the listing, not the decision. */
	pinned_at?: string;
}
export interface ProviderClaims {
	provider_id: string;
	provider_name: string;
	gone: ModelClaim[];
	stale: ModelClaim[];
	suspect: ModelClaim[];
	retired: ModelClaim[];
	/** Always sent (empty rather than null) by servers that know about the
	 *  operator pin, and absent entirely from ones that do not, which a rolling
	 *  deploy puts behind this dashboard. Read it through `?? []`. */
	pinned: ModelClaim[];
}
/** One failover group discovery disabled: `hotel/<display_model>` routing for it
 *  is dead until it is fixed. Operator-disabled groups never appear here. */
export interface GroupClaim {
	display_model: string;
	/** Members configured on the group. */
	member_count: number;
	/** Members whose model AND provider are both enabled right now. */
	routable_count: number;
	/** When discovery disabled it. */
	disabled_at: string;
}
/** GET /api/discovery/status. `claim_count` counts `gone` and `retired` models
 *  plus `group_claims`; `stale` and `suspect` are shown but never counted.
 *  `informational_unseen` skips entries whose only content is metadata
 *  (`updated`), so price churn cannot light the badge dot. */
export interface DiscoveryStatusResponse {
	claims: ProviderClaims[];
	group_claims: GroupClaim[];
	informational: DiscoveryChangeEntry[];
	claim_count: number;
	informational_unseen: number;
}
export interface DiscoverAllResult {
	provider_name: string;
	discovered: number;
	diff?: DiscoveryDiff;
	error?: string;
}
