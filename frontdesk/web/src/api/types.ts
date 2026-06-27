// Typed mirror of the Front Desk Go API (internal/frontdesk). Keep in step with
// store.go / poller.go / server.go.

export type MemberState = "active" | "drained";

export interface Member {
	id: string;
	name: string;
	url: string;
	state: MemberState;
	has_token: boolean;
	created_at: string;
	updated_at: string;
	// Set after add/edit when the admin token was saved but could not be
	// confirmed (member offline, or an older build). A refused token is a 400.
	token_warning?: string;
	// When Front Desk last applied config to this member (wizard or automatic),
	// RFC3339; absent until the first sync. last_config_sync_reason explains why
	// (e.g. the primary's config changed). Both drive the "Last Config Sync"
	// column on the Members tab.
	last_config_sync_at?: string;
	last_config_sync_reason?: string;
}

// AutoSyncConfig is the automatic config-propagation setup: a master on/off plus
// the designated source-of-truth member (empty when none is chosen).
export interface AutoSyncConfig {
	enabled: boolean;
	primary_id: string;
}

export interface HealthStatus {
	known: boolean;
	healthy: boolean;
	latency_ms: number;
	checked_at: string;
	error?: string;
}

export interface MemberStatus {
	health: HealthStatus;
	traefik_status?: string; // "UP" | "DOWN" | ""
	version?: string;
}

// memberView from listMembers: a Member plus its live poller status.
export interface MemberView extends Member {
	status: MemberStatus;
}

export interface Settings {
	health_poll_secs: number;
	traefik_poll_secs: number;
	traefik_stale_secs: number;
	event_retention_days: number;
	retry_attempts: number;
	sticky_enabled: boolean;
}

export type Severity = "info" | "success" | "warning" | "error";

export interface FdEvent {
	id: string;
	type: string;
	severity: string;
	source: string;
	message: string;
	metadata?: Record<string, unknown>;
	member_id?: string;
	created_at: string;
}

export interface EventsPage {
	events: FdEvent[];
	total: number;
}

// Per-member traffic metrics, as proxied by Front Desk from each member's
// /api/stats/timeseries (last hour, 5-minute buckets).
export interface MemberTrafficPoint {
	bucket: string;
	requests: number;
	errors: number;
}

export interface MemberTraffic {
	member_id: string;
	reachable: boolean;
	window_minutes: number;
	total_requests: number;
	total_errors: number;
	points: MemberTrafficPoint[];
}

// Per-member result of the config-sync POST the wizard runs.
export interface SyncResultItem {
	member_id: string;
	name: string;
	ok: boolean;
	error?: string;
}

export interface SyncResult {
	results: SyncResultItem[];
}

// --- Fleet sync wizard status (GET /api/fleet/status) ---
// One member's convergence state against the chosen primary, mirroring
// internal/frontdesk/fleetstatus.go. The wizard gates each step on these fields.
export interface FleetMemberStatus {
	member_id: string;
	name: string;
	reachable: boolean;
	has_token: boolean;
	// null = MASTER_KEY not evaluated: a keyless fleet (nothing to verify) or a
	// member that could not be probed. A non-null false is a real mismatch.
	master_key_matches: boolean | null;
	schema_ok: boolean;
	added: number;
	updated: number;
	removed: number;
	note?: string;
}

export interface FleetStatus {
	primary_id: string;
	primary_reachable: boolean;
	primary_note?: string;
	members: FleetMemberStatus[];
	// Host port the load balancer is published on (LB_PORT in the HA .env). The
	// wizard's Done step pairs it with the browser host to show where to send
	// /v1 traffic. Absent only if Front Desk was not told the port.
	lb_port?: string;
}

// Last successful fleet-sync wizard run (GET /api/fleet/last-sync). Absent
// (null/204) until the wizard converges at least one member.
export interface FleetSyncState {
	last_run_at: string;
	primary_id: string;
	primary_name: string;
}

// --- Admin authentication (passkeys + TOTP), Settings → Security ---

export interface WebAuthnCredential {
	id: string;
	name: string;
	transports: string[];
	created_at: string;
	aaguid: string;
	sign_count: number;
}

/** Admin-gated detail for the Security panel (not the polled public status). */
export interface TotpInfo {
	recovery_remaining: number;
	recovery_total: number;
	/** RFC3339 time a TOTP code was last accepted; absent if never used. */
	last_used_at?: string;
}

export interface TotpEnrollStart {
	uri: string;
	secret: string;
}

export interface TotpEnrollVerify {
	recovery_codes: string[];
	// Session token minted on enable so the admin stays logged in (the raw
	// FRONTDESK_TOKEN stops being a valid bearer once TOTP is on). Absent only if
	// the server could not mint one, in which case the user must re-login.
	token?: string;
}
