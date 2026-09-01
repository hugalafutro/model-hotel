export interface CacheHits {
	failover?: boolean | null;
	model?: boolean | null;
	provider?: boolean | null;
	key?: boolean | null;
	settings?: boolean | null;
}
export interface LogEntry {
	id: string;
	provider_id: string;
	provider_name: string;
	model_id: string;
	request_hash: string;
	status_code: number;
	latency_ms: number;
	duration_ms: number;
	ttft_ms: number;
	response_header_ms: number;
	proxy_overhead_ms: number;
	parse_ms: number;
	failover_lookup_ms: number;
	model_lookup_ms: number;
	provider_lookup_ms: number;
	key_decrypt_ms: number;
	dial_ms: number;
	settings_read_ms: number;
	cache_hits?: CacheHits | null;
	tokens_per_second: number | null;
	tokens_prompt: number;
	tokens_completion: number;
	tokens_prompt_cache_hit: number;
	tokens_prompt_cache_miss: number;
	tokens_completion_reasoning: number;
	streaming: boolean;
	state: string;
	virtual_key_name: string;
	virtual_key_deleted?: boolean;
	virtual_key_id?: string;
	/** Trusted-proxy-aware client address; "" or absent for rows predating it. */
	client_ip?: string;
	error_message: string;
	/** Machine-readable failure classification; "" or absent for legacy rows. */
	error_kind?: string;
	failover_attempt: number;
	created_at: string;
	resolved_model_id: string;
	endpoint_type: string;
	/** Per-attempt trail: one element per failover attempt, hedged probes and
	 * breaker skips included, in order. Absent for rows without one (legacy
	 * rows, rows an older member wrote, requests that never reached a
	 * candidate). The terminal attempt's values also live in the flat columns. */
	attempts?: AttemptRecord[];
}
export interface AttemptRecord {
	/** The loop's index (same numbering as failover_attempt); -1 marks a
	 * candidate the circuit breaker refused before any request was made. */
	attempt: number;
	provider_id: string;
	provider: string;
	model: string;
	/** Upstream HTTP status the attempt reached; absent when none was seen. */
	status?: number;
	error_kind?: string;
	/** At most 160 characters of the sanitized, credential-masked upstream error. */
	detail?: string;
	/** The rate-limit phrase-table entry a 429 matched, when one did. */
	phrase?: string;
	duration_ms: number;
	ttft_ms?: number;
	hedged?: boolean;
	/** What the attempt did to the circuit: charge, noop, success, alive,
	 * skipped, disabled; absent when the breaker was never consulted. */
	breaker?: string;
}
export interface AppLogEntry {
	id?: string;
	created_at?: string;
	timestamp: string;
	level: "info" | "warning" | "error";
	source: string;
	message: string;
	/** True when the message's attribute values use the backend's flattened
	 * encoding (spaces as \x20); gates display-side decoding. Absent/false on
	 * legacy rows and raw io.Writer lines, which render verbatim. */
	escaped?: boolean;
	/** Byte offset where the encoded attribute suffix begins; decoding applies
	 * only from here, so raw message text is never altered. */
	attrs_at?: number;
}
export interface LogsResponse {
	entries: LogEntry[];
	total: number;
	page: number;
	per_page: number;
}
export interface LogsCursorResponse {
	entries: LogEntry[];
	total: number;
	has_before: boolean;
	has_after: boolean;
}
export interface AppLogsCursorResponse {
	entries: AppLogEntry[];
	total: number;
	has_before: boolean;
	has_after: boolean;
	level_counts?: Record<string, number>;
	source_counts?: Record<string, number>;
}
// AuditEntry is one recorded admin action, served by GET /api/audit
// (admin-only). Request bodies are never recorded server-side.
export interface AuditEntry {
	id: string;
	created_at: string;
	actor: string;
	actor_role: string;
	method: string;
	route: string;
	path: string;
	entity_id?: string;
	status_code: number;
	remote_addr: string;
	// Current display name of the entity, resolved server-side at read time.
	// Absent when the entity was deleted or the route family is unmapped.
	entity_name?: string;
}
// AuditListResponse is the cursor-paginated audit page.
export interface AuditListResponse {
	entries: AuditEntry[];
	total: number;
	has_more: boolean;
	next_cursor?: string;
}
