import type {
	PublicKeyCredentialCreationOptionsJSON,
	PublicKeyCredentialRequestOptionsJSON,
} from "@simplewebauthn/browser";
import type {
	AlertEventDef,
	AlertStatus,
	AlertTargets,
	AuthSession,
	AutoSyncConfig,
	DeviceRole,
	EventsPage,
	FdEvent,
	FleetStatus,
	FleetSyncState,
	FleetVersionCheck,
	Member,
	MemberState,
	MemberTraffic,
	MemberView,
	ObservabilityStatus,
	OidcStatus,
	PairedDevice,
	PairStart,
	QuotaRefreshResult,
	QuotaSnapshot,
	Settings,
	SyncResult,
	TotpEnrollStart,
	TotpEnrollVerify,
	TotpInfo,
	VersionInfo,
	WebAuthnCredential,
} from "./types";

// Same-origin: the SPA is embedded in and served by the Front Desk binary.
// Auth rides an HttpOnly session cookie the server sets on login (raw
// FRONTDESK_TOKEN exchange, TOTP, or passkey); every request carries
// credentials: "same-origin" so the browser attaches it automatically.
export const API_BASE = "";

// Session state rides an HttpOnly fd_session cookie the page cannot read; the
// companion readable fd_csrf cookie is both the CSRF double-submit source and
// the client-visible "is logged in" signal.
const CSRF_COOKIE = "fd_csrf";

/** getCsrfToken reads the readable fd_csrf cookie, or null when absent. */
export function getCsrfToken(): string | null {
	const m = document.cookie.match(/(?:^|;\s*)fd_csrf=([^;]+)/);
	return m ? decodeURIComponent(m[1]) : null;
}

/** hasSession reports whether a login has left its readable cookie marker. */
export function hasSession(): boolean {
	return getCsrfToken() !== null;
}

// clearSessionHint locally expires the readable cookie so the UI drops to the
// login screen immediately; the server's Set-Cookie on logout/401 is the
// authoritative clear. Stays on document.cookie rather than the async Cookie
// Store API (unavailable in Safari/Firefox) since every caller here either
// reloads the page or throws right after this returns.
export function clearSessionHint(): void {
	// biome-ignore lint/suspicious/noDocumentCookie: must be synchronous; see the doc comment above.
	document.cookie = `${CSRF_COOKIE}=; path=/; max-age=0`;
}

export class ApiError extends Error {
	status: number;
	// Stable machine-readable code from a JSON error body ({code, error}), when the
	// endpoint provides one. Lets callers route on the code instead of matching
	// translatable English text. Undefined for plain-text error responses.
	code?: string;
	constructor(status: number, message: string, code?: string) {
		super(message);
		this.status = status;
		this.code = code;
		this.name = "ApiError";
	}
}

// Listeners notified when an authenticated request gets a 401 so the app can
// drop to the login screen instead of rendering a broken authed view.
type UnauthorizedListener = () => void;
const unauthorizedListeners = new Set<UnauthorizedListener>();
export function onUnauthorized(fn: UnauthorizedListener): () => void {
	unauthorizedListeners.add(fn);
	return () => unauthorizedListeners.delete(fn);
}
// Clear the session hint and notify listeners (the app falls back to login).
// Exported so the SSE stream, which uses raw fetch and bypasses request(), can
// trigger the same path on a 401, so the app drops to login instead of
// reconnecting a dead session.
export function notifyUnauthorized() {
	clearSessionHint();
	for (const fn of unauthorizedListeners) fn();
}

const SAFE_METHODS = new Set(["GET", "HEAD", "OPTIONS"]);

async function request<T>(path: string, init?: RequestInit): Promise<T> {
	const method = (init?.method ?? "GET").toUpperCase();
	const headers = new Headers(init?.headers);
	if (!SAFE_METHODS.has(method)) {
		const csrf = getCsrfToken();
		if (csrf) headers.set("X-CSRF-Token", csrf);
	}
	let resp: Response;
	try {
		resp = await fetch(`${API_BASE}${path}`, {
			...init,
			credentials: "same-origin",
			headers,
		});
	} catch {
		throw new ApiError(0, "network");
	}
	if (resp.status === 401) {
		notifyUnauthorized();
		throw new ApiError(401, "unauthorized");
	}
	if (!resp.ok) {
		const text = (await resp.text().catch(() => "")).trim();
		// Some endpoints return a JSON {code, error} body so the caller can route on
		// a stable code; others return plain text. Parse the coded form when present,
		// otherwise fall back to the raw text as the message.
		let message = text || `HTTP ${resp.status}`;
		let code: string | undefined;
		if (text.startsWith("{")) {
			try {
				const body = JSON.parse(text) as { code?: unknown; error?: unknown };
				if (typeof body.error === "string" && body.error) message = body.error;
				if (typeof body.code === "string") code = body.code;
			} catch {
				// Not JSON after all; keep the raw text as the message.
			}
		}
		throw new ApiError(resp.status, message, code);
	}
	if (resp.status === 204) return undefined as T;
	const ct = resp.headers.get("content-type") ?? "";
	if (!ct.includes("application/json")) return undefined as T;
	return (await resp.json()) as T;
}

const jsonInit = (method: string, body: unknown): RequestInit => ({
	method,
	headers: { "Content-Type": "application/json" },
	body: JSON.stringify(body),
});

export const api = {
	listMembers: () => request<MemberView[]>("/api/members"),
	createMember: (name: string, url: string, token: string) =>
		request<Member>("/api/members", jsonInit("POST", { name, url, token })),
	patchMember: (id: string, patch: { name?: string; token?: string }) =>
		request<Member>(
			`/api/members/${encodeURIComponent(id)}`,
			jsonInit("PATCH", patch),
		),
	// Only non-primary members can be deleted; the backend refuses a primary with
	// 409 (change it via the Fleet Sync wizard instead), so no token is sent here.
	deleteMember: (id: string) =>
		request<void>(`/api/members/${encodeURIComponent(id)}`, {
			method: "DELETE",
		}),
	setMemberState: (id: string, state: MemberState) =>
		request<Member>(
			`/api/members/${encodeURIComponent(id)}/state`,
			jsonInit("POST", { state }),
		),

	getSettings: () => request<Settings>("/api/settings"),
	// Quota snapshots proxied from the designated fleet primary. An empty list is
	// the authoritative "no primary designated" answer; any non-2xx means we could
	// not ask, and callers must keep their last-good data rather than blank it.
	getQuota: () => request<{ quota: QuotaSnapshot[] }>("/api/quota"),
	// Forces the primary to re-poll its quota providers upstream. Monitor tier, so
	// the same endpoint Bellhop's pull-to-refresh uses.
	refreshQuota: () =>
		request<QuotaRefreshResult>("/api/quota/refresh", { method: "POST" }),
	// Best-effort server-side session revoke for logout (manual or idle). A raw
	// FRONTDESK_TOKEN bearer has no session row, so the server no-ops and returns 200.
	logout: () => request<void>("/api/logout", { method: "POST" }),
	// Trades the raw FRONTDESK_TOKEN for the HttpOnly session cookie pair; the
	// raw token never persists in the browser. 400 with code-less "use TOTP
	// login" means 2FA is on and the totpLogin flow applies instead.
	adminExchange: (token: string) =>
		request<void>(
			"/api/auth/admin-exchange",
			jsonInit("POST", { admin_token: token }),
		),
	// Running-build identity for the footer (version + short commit SHA).
	getVersion: () => request<VersionInfo>("/api/version"),
	// Read-only log-export integration status for the Observability panel,
	// derived server-side from the process environment.
	getObservability: () => request<ObservabilityStatus>("/api/observability"),
	// Partial body: the server merges onto the stored row, so each panel PUTs only
	// the fields it owns and never clobbers the other's (or erases the secret).
	putSettings: (s: Partial<Settings>) =>
		request<void>("/api/settings", jsonInit("PUT", s)),

	// Outbound Apprise alerting (Settings -> Alerts panel).
	getAlertEvents: () => request<AlertEventDef[]>("/api/alert/events"),
	getAlertStatus: () => request<AlertStatus>("/api/alert/status"),
	probeAlert: (apiUrl: string) =>
		request<AlertStatus>(
			"/api/alert/probe",
			jsonInit("POST", { api_url: apiUrl }),
		),
	// No body: test the saved configuration. {api_url, targets}: test one or more
	// explicit destinations through an explicit URL before anything is saved.
	// The no-body form sends no body and no Content-Type, which is how the
	// server tells "use what is stored" from "use exactly this".
	testAlert: (body?: { api_url?: string; targets?: string[] }) =>
		request<void>(
			"/api/alert/test",
			body ? jsonInit("POST", body) : { method: "POST" },
		),
	getAlertTargets: () => request<AlertTargets>("/api/alert/targets"),

	listEvents: (params: URLSearchParams) =>
		request<EventsPage>(`/api/events?${params.toString()}`),

	memberTraffic: (id: string) =>
		request<MemberTraffic>(`/api/members/${encodeURIComponent(id)}/traffic`),

	// Bellhop device pairing (Settings -> Paired devices panel). pairStart mints
	// a one-time short-TTL pairing code (admin only); the phone exchanges it at
	// the public POST /api/pair. Devices are listed and revoked here.
	pairStart: (role: DeviceRole) =>
		request<PairStart>("/api/pair/start", jsonInit("POST", { role })),
	// Is this specific code still live? Lets the panel tell "my code was used"
	// apart from "some other device paired" so it dismisses the right QR.
	pairStatus: (code: string) =>
		request<{ outstanding: boolean }>(
			"/api/pair/status",
			jsonInit("POST", { code }),
		),
	getDevices: () => request<PairedDevice[]>("/api/devices"),
	revokeDevice: (id: string) =>
		request<{ success: boolean }>(`/api/devices/${encodeURIComponent(id)}`, {
			method: "DELETE",
		}),

	// Browser-session hygiene (Settings → Active sessions): the admin
	// identity's live sessions, a per-row sign-out, and the bulk revoke that
	// keeps only the calling session.
	getSessions: () => request<{ sessions: AuthSession[] }>("/api/auth/sessions"),
	revokeSession: (id: string) =>
		request<void>(`/api/auth/sessions/${encodeURIComponent(id)}`, {
			method: "DELETE",
		}),
	revokeOtherSessions: () =>
		request<{ revoked: number }>("/api/auth/sessions/revoke-others", {
			method: "POST",
		}),

	// One probe powers the whole sync wizard: per-member reachability, MASTER_KEY
	// match, and the config diff against the chosen primary.
	fleetStatus: (primaryId: string) =>
		request<FleetStatus>(
			`/api/fleet/status?primary=${encodeURIComponent(primaryId)}`,
		),
	// Last successful wizard run (timestamp + primary), or null when never run.
	fleetLastSync: () => request<FleetSyncState | null>("/api/fleet/last-sync"),

	// Automatic config propagation: read and update the toggle + designated
	// primary. Front Desk's poller watches that primary and re-syncs the fleet
	// when its config changes.
	getAutoSync: () => request<AutoSyncConfig>("/api/fleet/autosync"),
	// confirmToken re-authenticates the operator when repointing or clearing an
	// already-configured primary; the backend rejects that change without it.
	putAutoSync: (cfg: AutoSyncConfig, confirmToken?: string) =>
		request<AutoSyncConfig>(
			"/api/fleet/autosync",
			jsonInit(
				"PUT",
				confirmToken ? { ...cfg, confirm_token: confirmToken } : cfg,
			),
		),

	configSync: (primaryId: string) =>
		request<SyncResult>(
			"/api/config/sync",
			jsonInit("POST", { primary_id: primaryId }),
		),

	// Re-poll member versions and report the ones that differ from the
	// primary's. Powers the wizard's version gate and its Refresh button.
	fleetVersionCheck: (primaryId: string) =>
		request<FleetVersionCheck>(
			"/api/fleet/version-check",
			jsonInit("POST", { primary_id: primaryId }),
		),

	// Auth (unauthenticated except where noted).
	totpStatus: () =>
		request<{ enabled: boolean; enabled_at?: string }>("/api/totp/status"),
	totpLogin: (token: string, code: string) =>
		request<{ success: boolean }>(
			"/api/totp/login",
			jsonInit("POST", { token, code }),
		),
	webauthnAvailable: () =>
		request<{ enabled: boolean; has_credentials: boolean }>(
			"/api/webauthn/available",
		),
	// OIDC SSO login status (public): drives the "Sign in with SSO" button.
	oidcStatus: () => request<OidcStatus>("/api/auth/oidc/status"),
	webauthnLoginStart: () =>
		request<{
			session_id: string;
			options: PublicKeyCredentialRequestOptionsJSON;
		}>("/api/webauthn/login/start", {
			method: "POST",
		}),
	webauthnLoginFinish: (sessionId: string, credential: unknown) =>
		request<{ success: boolean }>(
			"/api/webauthn/login/finish",
			jsonInit("POST", { session_id: sessionId, credential }),
		),

	// Passkey management (admin-gated; bearer attached automatically once logged in).
	webauthnRegisterStart: () =>
		request<{
			session_id: string;
			options: PublicKeyCredentialCreationOptionsJSON;
		}>("/api/webauthn/register/start", { method: "POST" }),
	webauthnRegisterFinish: (sessionId: string, credential: unknown) =>
		request<{ success: boolean }>(
			"/api/webauthn/register/finish",
			jsonInit("POST", { session_id: sessionId, credential }),
		),
	webauthnListCredentials: () =>
		request<WebAuthnCredential[]>("/api/webauthn/credentials"),
	webauthnRenameCredential: (id: string, name: string) =>
		request<void>(
			`/api/webauthn/credentials/${encodeURIComponent(id)}`,
			jsonInit("PATCH", { name }),
		),
	webauthnDeleteCredential: (id: string) =>
		request<void>(`/api/webauthn/credentials/${encodeURIComponent(id)}`, {
			method: "DELETE",
		}),

	// TOTP management (admin-gated).
	totpInfo: () => request<TotpInfo>("/api/totp/info"),
	totpEnrollStart: () =>
		request<TotpEnrollStart>("/api/totp/enroll/start", { method: "POST" }),
	totpEnrollVerify: (code: string) =>
		request<TotpEnrollVerify>(
			"/api/totp/enroll/verify",
			jsonInit("POST", { code }),
		),
	totpDisable: (code: string) =>
		request<void>("/api/totp/disable", jsonInit("POST", { code })),
};

// Re-export so consumers importing the client get the event type without a
// second import path.
export type { FdEvent };
