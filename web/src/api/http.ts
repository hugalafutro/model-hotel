import { readCookie } from "@web-shared/cookies";

export const API_BASE = "";

// ── Internal helpers ────────────────────────────────────────────────

// ApiError carries the HTTP status so callers can branch on it (e.g. a 429
// throttle vs a 401 on the login screen). instanceof Error stays true and the
// message is unchanged, so existing catch blocks keep working. `code` is a
// stable machine-readable failure code parsed from a JSON `{code, error}`
// error body (see fetchOK below); absent for endpoints that don't send one or
// for a plain-text error body.
export class ApiError extends Error {
	readonly status: number;
	readonly code?: string;
	/** Remaining fields of a coded error body, for callers that phrase the
	 * failure themselves (e.g. which server answered a provider probe). */
	readonly details?: Record<string, unknown>;
	constructor(
		message: string,
		status: number,
		code?: string,
		details?: Record<string, unknown>,
	) {
		super(message);
		this.name = "ApiError";
		this.status = status;
		this.code = code;
		this.details = details;
	}
}

export async function fetchOK(
	url: string,
	options?: RequestInit,
	errorPrefix = "Request failed",
): Promise<Response> {
	// Same-origin credentials so the httpOnly session cookie rides along on every
	// dashboard call. (Same-origin is the fetch default, but we set it explicitly
	// so the intent is obvious and survives any future default change.)
	const init: RequestInit = { credentials: "same-origin", ...options };
	// The CSRF token only guards state-changing requests. getAuthHeaders() is
	// shared by GET and mutating callers, so strip X-CSRF-Token here for safe
	// methods (GET/HEAD) rather than leaking the token on read-only calls. The
	// original header shape is preserved (plain object stays a plain object).
	const method = (init.method ?? "GET").toUpperCase();
	if ((method === "GET" || method === "HEAD") && init.headers) {
		if (init.headers instanceof Headers) {
			init.headers.delete("X-CSRF-Token");
		} else if (Array.isArray(init.headers)) {
			init.headers = init.headers.filter(
				([key]) => key.toLowerCase() !== "x-csrf-token",
			);
		} else {
			const rest = { ...(init.headers as Record<string, string>) };
			delete rest["X-CSRF-Token"];
			init.headers = rest;
		}
	}
	const response = await fetch(url, init);
	if (!response.ok) {
		// A 401 means the session cookie is gone or invalid. Drop the client-side
		// auth signal so isAuthenticated() flips false; the surfaced ApiError lets
		// callers show a session-expired state (the SSE stream forces the reload
		// to the login screen).
		if (response.status === 401) clearAuth();
		const text = await response.text();
		// A coded error body ({code, error}, e.g. from writeCodedError) lets the
		// caller branch on `code` instead of matching English text; anything else
		// (plain text, or JSON without a string `code`) falls back to the
		// unchanged bare-text message.
		let code: string | undefined;
		let details: Record<string, unknown> | undefined;
		let message = `${errorPrefix}: ${response.status} ${text}`;
		if (text.startsWith("{")) {
			try {
				const body = JSON.parse(text) as {
					code?: string;
					error?: string;
				} & Record<string, unknown>;
				if (typeof body.code === "string") {
					code = body.code;
					details = body;
					message = `${errorPrefix}: ${response.status} ${body.error ?? text}`;
				}
			} catch {
				// Not valid JSON despite the leading brace; keep the raw-text message.
			}
		}
		throw new ApiError(message, response.status, code, details);
	}
	return response;
}

export async function fetchJSON<T>(
	url: string,
	options?: RequestInit,
	errorPrefix = "Request failed",
): Promise<T> {
	const response = await fetchOK(url, options, errorPrefix);
	return response.json();
}

/** Server wall-clock (epoch ms) parsed from a response's `Date` header, or null
 * when it is absent or unparseable. The embedded dashboard is same-origin, so
 * every response exposes `Date`; it lets time-sensitive UI reason on the
 * server's clock instead of a possibly-skewed browser clock. */
export function serverNowFromResponse(response: Response): number | null {
	const raw = response.headers.get("Date");
	if (!raw) return null;
	const ms = Date.parse(raw);
	return Number.isNaN(ms) ? null : ms;
}

/** Like fetchJSON, but also returns the server's wall-clock from the response's
 * `Date` header (null when unavailable, e.g. under a mock without the header).
 * Used where the browser clock cannot be trusted as "now". */
export async function fetchJSONWithServerNow<T>(
	url: string,
	options?: RequestInit,
	errorPrefix = "Request failed",
): Promise<{ data: T; serverNowMs: number | null }> {
	const response = await fetchOK(url, options, errorPrefix);
	const serverNowMs = serverNowFromResponse(response);
	return { data: (await response.json()) as T, serverNowMs };
}

export function buildQueryString(
	params: Record<string, string | number | boolean | undefined>,
): string {
	const sp = new URLSearchParams();
	for (const [key, value] of Object.entries(params)) {
		if (value !== undefined && value !== null) sp.set(key, String(value));
	}
	return sp.toString();
}

export function buildUrl(
	path: string,
	params?: Record<string, string | number | boolean | undefined>,
): string {
	if (!params) return `${API_BASE}${path}`;
	const qs = buildQueryString(params);
	return qs ? `${API_BASE}${path}?${qs}` : `${API_BASE}${path}`;
}

// ── API ─────────────────────────────────────────────────────────────

// Cookie-session auth. The session rides in an httpOnly `mh_session` cookie the
// browser attaches automatically on same-origin requests; JS cannot read it. A
// companion readable `mh_csrf` cookie is the client-visible "is logged in"
// signal and is echoed back in the X-CSRF-Token header on mutating requests so
// the server can reject cross-site writes. No bearer token is ever stored or
// sent. CSRF_COOKIE names that readable cookie once, so the read below and the
// expiry write in clearAuth cannot come to mean different cookies.
const CSRF_COOKIE = "mh_csrf";

/** getCsrfToken reads the readable `mh_csrf` cookie, or null when absent. */
export function getCsrfToken(): string | null {
	return readCookie(CSRF_COOKIE);
}

/** isAuthenticated reports whether the dashboard session cookie pair is present,
 * derived from the readable CSRF cookie (the httpOnly session cookie can't be
 * observed from JS). */
export function isAuthenticated(): boolean {
	return getCsrfToken() !== null;
}

/** clearAuth expires the readable CSRF cookie so isAuthenticated() flips false.
 * The httpOnly session cookie is cleared server-side on logout / on a 401; this
 * drops the client-visible auth signal immediately.
 *
 * The write stays on document.cookie rather than the Cookie Store API that
 * noDocumentCookie suggests: cookieStore.delete() is async and unavailable in
 * Safari and Firefox, and every caller either reloads the page (Layout logout,
 * the SSE 401 path) or throws right after this returns, so a promise-based
 * delete could lose the race and leave the stale signal readable on the next
 * page load. */
export function clearAuth(): void {
	// biome-ignore lint/suspicious/noDocumentCookie: must be synchronous; see the doc comment above.
	document.cookie = `${CSRF_COOKIE}=; path=/; max-age=0`;
}

/** getAuthHeaders returns the headers for an authenticated mutating request:
 * a JSON content type plus the CSRF token echoed from the readable cookie. It
 * never throws and never sends an Authorization bearer; the session travels in
 * the cookie. Safe (GET) requests may reuse it harmlessly — the server only
 * validates the CSRF header on mutating methods. */
export function getAuthHeaders(): Record<string, string> {
	const headers: Record<string, string> = {
		"Content-Type": "application/json",
	};
	const csrf = getCsrfToken();
	if (csrf) headers["X-CSRF-Token"] = csrf;
	return headers;
}
