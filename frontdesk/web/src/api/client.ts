import type { PublicKeyCredentialRequestOptionsJSON } from "@simplewebauthn/browser";
import type {
	EventsPage,
	FdEvent,
	Member,
	MemberState,
	MemberTraffic,
	MemberView,
	ResetResult,
	Settings,
	SyncPreview,
	SyncResult,
} from "./types";

// Same-origin: the SPA is embedded in and served by the Front Desk binary.
export const API_BASE = "";

const TOKEN_KEY = "fdAuthToken";

// The bearer token is either the raw FRONTDESK_TOKEN (valid only while TOTP is
// off) or a session token minted by a passkey / TOTP login. It is the same
// header either way; the server (RequireAdminOrSession) decides which is valid.
export function getAuthToken(): string {
	try {
		return localStorage.getItem(TOKEN_KEY) ?? "";
	} catch {
		return "";
	}
}

export function setAuthToken(token: string) {
	try {
		localStorage.setItem(TOKEN_KEY, token);
	} catch {
		/* private mode: token lives only for this page load */
	}
}

export function clearAuthToken() {
	try {
		localStorage.removeItem(TOKEN_KEY);
	} catch {
		/* ignore */
	}
}

export class ApiError extends Error {
	status: number;
	constructor(status: number, message: string) {
		super(message);
		this.status = status;
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
// Drop the stored token and notify listeners (the app falls back to login).
// Exported so the SSE stream, which uses raw fetch and bypasses request(), can
// trigger the same path on a 401 instead of reconnecting with a dead token.
export function notifyUnauthorized() {
	clearAuthToken();
	for (const fn of unauthorizedListeners) fn();
}

function authHeaders(extra?: HeadersInit): HeadersInit {
	return { Authorization: `Bearer ${getAuthToken()}`, ...extra };
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
	let resp: Response;
	try {
		resp = await fetch(`${API_BASE}${path}`, {
			...init,
			headers: authHeaders(init?.headers),
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
		throw new ApiError(resp.status, text || `HTTP ${resp.status}`);
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
	putSettings: (s: Settings) =>
		request<void>("/api/settings", jsonInit("PUT", s)),

	listEvents: (params: URLSearchParams) =>
		request<EventsPage>(`/api/events?${params.toString()}`),

	memberTraffic: (id: string) =>
		request<MemberTraffic>(`/api/members/${encodeURIComponent(id)}/traffic`),

	syncPreview: (primaryId: string) =>
		request<SyncPreview>(
			`/api/admin-token/preview?primary=${encodeURIComponent(primaryId)}`,
		),
	syncRun: (primaryId: string) =>
		request<SyncResult>(
			"/api/admin-token/sync",
			jsonInit("POST", { primary_id: primaryId }),
		),
	resetAdminToken: () =>
		request<ResetResult>("/api/admin-token/reset", { method: "POST" }),

	// Auth (unauthenticated except where noted).
	totpStatus: () => request<{ enabled: boolean }>("/api/totp/status"),
	totpLogin: (token: string, code: string) =>
		request<{ token: string }>(
			"/api/totp/login",
			jsonInit("POST", { token, code }),
		),
	webauthnAvailable: () =>
		request<{ enabled: boolean }>("/api/webauthn/available"),
	webauthnLoginStart: () =>
		request<{
			session_id: string;
			options: PublicKeyCredentialRequestOptionsJSON;
		}>("/api/webauthn/login/start", {
			method: "POST",
		}),
	webauthnLoginFinish: (sessionId: string, credential: unknown) =>
		request<{ token: string }>(
			"/api/webauthn/login/finish",
			jsonInit("POST", { session_id: sessionId, credential }),
		),
};

// Re-export so consumers importing the client get the event type without a
// second import path.
export type { FdEvent };
