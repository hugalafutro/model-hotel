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

// Admin-token sync preview / result types.
export type SyncDisposition = "overwrite" | "matches" | "blocked";

export interface SyncPreviewItem {
	member_id: string;
	name: string;
	disposition: SyncDisposition;
}

export interface SyncPreview {
	primary_id: string;
	items: SyncPreviewItem[];
}

export interface SyncResultItem {
	member_id: string;
	name: string;
	ok: boolean;
	error?: string;
}

export interface SyncResult {
	results: SyncResultItem[];
}

export interface ResetResult {
	token: string;
	results: SyncResultItem[];
}
