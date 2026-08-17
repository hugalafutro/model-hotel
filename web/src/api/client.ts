import type {
	AlertEventDef,
	AlertStatus,
	AlertTargets,
	AppLogsCursorResponse,
	AuditListResponse,
	AuthSession,
	AuthStatus,
	BackupClassification,
	BackupEntry,
	CandidateModel,
	CircuitBreakerResetResult,
	CircuitBreakerStatus,
	CreateFailoverGroupRequest,
	CreateProviderRequest,
	DashboardUser,
	DeepSeekBalance,
	DemoLogin,
	DiscoverAllResult,
	DiscoveryChangesResponse,
	DiscoveryDiff,
	DiscoveryStatusResponse,
	FailoverGroup,
	FailoverListResponse,
	GithubStatus,
	KimiCodeQuotaResponse,
	LogEntry,
	LogsCursorResponse,
	LogsResponse,
	Me,
	MiniMaxQuotaResponse,
	Model,
	ModelsCursorResponse,
	NanoGPTUsage,
	NeuralWattQuotaResponse,
	OidcStatus,
	OllamaCloudAccount,
	OpenRouterBalance,
	Provider,
	ProviderDistributionStats,
	PublicConfig,
	Stats,
	SyncResult,
	SystemStats,
	TimeSeriesStats,
	TotpEnrollStart,
	TotpEnrollVerify,
	TotpInfo,
	TotpLoginResponse,
	TotpStatus,
	UpdateFailoverGroupRequest,
	UpdateProviderRequest,
	UserTotpStatus,
	UserUpsertRequest,
	VirtualKey,
	ZAICodingQuotaResponse,
} from "./types";

export interface AppLogEntry {
	timestamp: string;
	level: "info" | "warning" | "error";
	source: string;
	message: string;
}

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

async function fetchOK(
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

async function fetchJSON<T>(
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
		if (value !== undefined && value !== null) {
			sp.set(key, String(value));
		}
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
// sent.

/** getCsrfToken reads the readable `mh_csrf` cookie, or null when absent. */
export function getCsrfToken(): string | null {
	const m = document.cookie.match(/(?:^|;\s*)mh_csrf=([^;]+)/);
	return m ? decodeURIComponent(m[1]) : null;
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
	document.cookie = "mh_csrf=; path=/; max-age=0";
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

export const api = {
	// Unauthenticated: read before login and inside the dashboard. No auth
	// headers so it works on the login screen too.
	publicConfig: {
		get: async (): Promise<PublicConfig> => {
			return fetchJSON<PublicConfig>(
				`${API_BASE}/api/public-config`,
				undefined,
				"Failed to fetch public config",
			);
		},
	},
	// Unauthenticated: read on the login screen. Returns an empty token unless
	// the server runs as a demo with the token-display feature enabled.
	demoLogin: {
		get: async (): Promise<DemoLogin> => {
			return fetchJSON<DemoLogin>(
				`${API_BASE}/api/demo-login`,
				undefined,
				"Failed to fetch demo login",
			);
		},
	},
	providers: {
		list: async (): Promise<Provider[]> => {
			return fetchJSON<Provider[]>(
				`${API_BASE}/api/providers`,
				{
					headers: getAuthHeaders(),
				},
				"Failed to fetch providers",
			);
		},
		create: async (data: CreateProviderRequest): Promise<Provider> => {
			return fetchJSON<Provider>(
				`${API_BASE}/api/providers`,
				{
					method: "POST",
					headers: getAuthHeaders(),
					body: JSON.stringify(data),
				},
				"Failed to create provider",
			);
		},
		delete: async (id: string): Promise<void> => {
			const response = await fetch(`${API_BASE}/api/providers/${id}`, {
				method: "DELETE",
				headers: getAuthHeaders(),
			});
			if (!response.ok) {
				throw new Error("Failed to delete provider");
			}
		},
		update: async (
			id: string,
			data: UpdateProviderRequest,
		): Promise<Provider> => {
			return fetchJSON<Provider>(
				`${API_BASE}/api/providers/${id}`,
				{
					method: "PUT",
					headers: getAuthHeaders(),
					body: JSON.stringify(data),
				},
				"Failed to update provider",
			);
		},
		discover: async (
			id: string,
		): Promise<{ discovered: number; diff: DiscoveryDiff }> => {
			return fetchJSON<{ discovered: number; diff: DiscoveryDiff }>(
				`${API_BASE}/api/providers/${id}/discover`,
				{
					method: "POST",
					headers: getAuthHeaders(),
				},
				"Failed to discover models",
			);
		},
		discoverAll: async (): Promise<{
			succeeded: number;
			failed: number;
			discovered: number;
			results: DiscoverAllResult[];
		}> => {
			return fetchJSON<{
				succeeded: number;
				failed: number;
				discovered: number;
				results: DiscoverAllResult[];
			}>(
				`${API_BASE}/api/providers/discover-all`,
				{
					method: "POST",
					headers: getAuthHeaders(),
				},
				"Failed to discover all",
			);
		},
		refreshQuotas: async (): Promise<{
			refreshed: number;
			failed: number;
			skipped: number;
			results: {
				provider_name: string;
				provider_type: string;
				refreshed: boolean;
				error?: string;
			}[];
		}> => {
			return fetchJSON<{
				refreshed: number;
				failed: number;
				skipped: number;
				results: {
					provider_name: string;
					provider_type: string;
					refreshed: boolean;
					error?: string;
				}[];
			}>(
				`${API_BASE}/api/providers/refresh-quotas`,
				{
					method: "POST",
					headers: getAuthHeaders(),
				},
				"Failed to refresh quotas",
			);
		},
		getUsage: async (
			id: string,
		): Promise<
			| NanoGPTUsage
			| ZAICodingQuotaResponse
			| KimiCodeQuotaResponse
			| MiniMaxQuotaResponse
		> => {
			return fetchJSON<
				| NanoGPTUsage
				| ZAICodingQuotaResponse
				| KimiCodeQuotaResponse
				| MiniMaxQuotaResponse
			>(
				`${API_BASE}/api/providers/${id}/usage`,
				{
					headers: getAuthHeaders(),
				},
				"Failed to fetch usage",
			);
		},
		getBalance: async (id: string): Promise<DeepSeekBalance> => {
			return fetchJSON<DeepSeekBalance>(
				`${API_BASE}/api/providers/${id}/balance`,
				{
					headers: getAuthHeaders(),
				},
				"Failed to fetch balance",
			);
		},
		getOpenRouterBalance: async (id: string): Promise<OpenRouterBalance> => {
			return fetchJSON<OpenRouterBalance>(
				`${API_BASE}/api/providers/${id}/usage`,
				{
					headers: getAuthHeaders(),
				},
				"Failed to fetch OpenRouter balance",
			);
		},
		getNeuralWattQuota: async (
			id: string,
		): Promise<NeuralWattQuotaResponse | null> => {
			const response = await fetchOK(
				`${API_BASE}/api/providers/${id}/usage`,
				{ headers: getAuthHeaders() },
				"Failed to fetch NeuralWatt quota",
			);
			if (response.status === 204) return null;
			return response.json();
		},
		getOllamaCloudAccount: async (id: string): Promise<OllamaCloudAccount> => {
			return fetchJSON<OllamaCloudAccount>(
				`${API_BASE}/api/providers/${id}/account`,
				{
					headers: getAuthHeaders(),
				},
				"Failed to fetch Ollama Cloud account",
			);
		},
	},

	discovery: {
		// Current discrepancy state plus the informational change feed. Pass
		// review=true only from the modal-open fetch: the server stamps the
		// last-reviewed marker on that variant, and the badge poll must not
		// consume it.
		status: async (review = false): Promise<DiscoveryStatusResponse> => {
			return fetchJSON<DiscoveryStatusResponse>(
				`${API_BASE}/api/discovery/status${review ? "?review=1" : ""}`,
				{ headers: getAuthHeaders() },
				"Failed to load discovery status",
			);
		},
		// Suppress (or, with dismissed=false, restore) a discrepancy.
		// Dismiss only. There is deliberately no un-dismiss: a dismissal self-heals
		// when discovery next sights the model, which is the only reversal the modal
		// needs.
		dismiss: async (
			providerId: string,
			modelIds: string[],
		): Promise<{ dismissed: string[]; updated: number }> => {
			return fetchJSON<{ dismissed: string[]; updated: number }>(
				`${API_BASE}/api/discovery/dismiss`,
				{
					method: "POST",
					headers: getAuthHeaders(),
					body: JSON.stringify({
						provider_id: providerId,
						model_ids: modelIds,
					}),
				},
				"Failed to dismiss discovery claims",
			);
		},
		// Hand pinned models back to automatic management. Unpin only, for the same
		// reason dismiss is one-way: the pin direction is an operator enable on the
		// model itself, not something the discrepancy modal invents.
		//
		// The response names the rows actually cleared and carries no `updated`
		// count: the server 404s when nothing matched, so a 200 that names fewer
		// models than were asked for is the only partial result there is.
		unpin: async (
			providerId: string,
			modelIds: string[],
		): Promise<{ unpinned: string[] }> => {
			return fetchJSON<{ unpinned: string[] }>(
				`${API_BASE}/api/discovery/unpin`,
				{
					method: "POST",
					headers: getAuthHeaders(),
					body: JSON.stringify({
						provider_id: providerId,
						model_ids: modelIds,
					}),
				},
				"Failed to unpin discovery claims",
			);
		},
		ackChanges: async (): Promise<DiscoveryChangesResponse> => {
			return fetchJSON<DiscoveryChangesResponse>(
				`${API_BASE}/api/discovery/changes/ack`,
				{
					method: "POST",
					headers: getAuthHeaders(),
				},
				"Failed to acknowledge discovery changes",
			);
		},
	},

	models: {
		list: async (providerId?: string): Promise<Model[]> => {
			const url = providerId
				? `${API_BASE}/api/models?provider_id=${providerId}`
				: `${API_BASE}/api/models`;
			return fetchJSON<Model[]>(
				url,
				{
					headers: getAuthHeaders(),
				},
				"Failed to fetch models",
			);
		},
		cursor: async (params: {
			cursor?: string;
			direction: "after" | "before";
			limit: number;
			sort_by?: string;
			sort_dir?: string;
			provider_id?: string;
			search?: string;
			capabilities?: string;
			outputs?: string;
		}): Promise<ModelsCursorResponse> => {
			const sp = new URLSearchParams();
			if (params.cursor) sp.set("cursor", params.cursor);
			sp.set("direction", params.direction);
			sp.set("limit", String(params.limit));
			if (params.sort_by) sp.set("sort_by", params.sort_by);
			if (params.sort_dir) sp.set("sort_dir", params.sort_dir);
			if (params.provider_id) sp.set("provider_id", params.provider_id);
			if (params.search) sp.set("search", params.search);
			if (params.capabilities) sp.set("capabilities", params.capabilities);
			if (params.outputs) sp.set("outputs", params.outputs);
			return fetchJSON<ModelsCursorResponse>(
				`${API_BASE}/api/models/cursor?${sp.toString()}`,
				{ headers: getAuthHeaders() },
				"Failed to fetch models (cursor)",
			);
		},
		update: async (
			id: string,
			data: {
				display_name?: string;
				context_length?: number | null;
				max_output_tokens?: number | null;
				input_price_per_million?: number | null;
				input_price_per_million_cache_hit?: number | null;
				output_price_per_million?: number | null;
				/** false clears the operator price pin and nulls the prices so the
				 *  next discovery scan re-derives them from source. */
				price_customized?: boolean;
				enabled?: boolean;
			},
		): Promise<Model> => {
			return fetchJSON<Model>(
				`${API_BASE}/api/models/${id}`,
				{
					method: "PATCH",
					headers: getAuthHeaders(),
					body: JSON.stringify(data),
				},
				"Failed to update model",
			);
		},
		test: async (
			id: string,
			// allowDisabled lets the failover "Retry N/A" action probe a disabled
			// model; the Models page test button omits it (enabled models only).
			allowDisabled = false,
		): Promise<{
			success: boolean;
			streaming: boolean;
			ttft_ms: number;
			duration_ms: number;
			response: string;
			error?: string;
		}> => {
			return fetchJSON<{
				success: boolean;
				streaming: boolean;
				ttft_ms: number;
				duration_ms: number;
				response: string;
				error?: string;
			}>(
				`${API_BASE}/api/models/${id}/test${allowDisabled ? "?allow_disabled=true" : ""}`,
				{
					method: "POST",
					headers: getAuthHeaders(),
				},
				"Test failed",
			);
		},
		delete: async (id: string): Promise<void> => {
			const response = await fetch(`${API_BASE}/api/models/${id}`, {
				method: "DELETE",
				headers: getAuthHeaders(),
			});
			if (!response.ok) {
				throw new Error("Failed to delete model");
			}
		},
		// Delete many models in one request. Deleting one HTTP DELETE per model
		// stampedes the admin IP rate limiter, so bulk selections go through this
		// single endpoint instead. deleted may be < requested when some IDs were
		// already gone (idempotent).
		bulkDelete: async (
			ids: string[],
		): Promise<{ requested: number; deleted: number }> => {
			return fetchJSON<{ requested: number; deleted: number }>(
				`${API_BASE}/api/models/bulk-delete`,
				{
					method: "POST",
					headers: getAuthHeaders(),
					body: JSON.stringify({ ids }),
				},
				"Failed to delete models",
			);
		},
	},

	logs: {
		list: async (
			params: {
				page?: number;
				per_page?: number;
				model_id?: string;
				provider_id?: string;
				status_code?: string;
				endpoint_type?: string;
				from?: string;
				to?: string;
				sort_by?: string;
				sort_dir?: string;
				/** Admin-only filter: scope to keys owned by this user. */
				owner_user_id?: string;
			} = {},
		): Promise<LogsResponse> => {
			return fetchJSON<LogsResponse>(
				buildUrl("/api/logs", {
					page: params.page,
					per_page: params.per_page,
					model_id: params.model_id,
					provider_id: params.provider_id,
					status_code: params.status_code,
					endpoint_type: params.endpoint_type,
					from: params.from,
					to: params.to,
					sort_by: params.sort_by,
					sort_dir: params.sort_dir,
					owner_user_id: params.owner_user_id,
				}),
				{ headers: getAuthHeaders() },
				"Failed to fetch logs",
			);
		},
		get: async (id: string): Promise<LogEntry> => {
			return fetchJSON<LogEntry>(
				`${API_BASE}/api/logs/${encodeURIComponent(id)}`,
				{ headers: getAuthHeaders() },
				"Failed to fetch log",
			);
		},
		purge: async (olderThan: string): Promise<void> => {
			const response = await fetch(`${API_BASE}/api/logs/purge`, {
				method: "DELETE",
				headers: getAuthHeaders(),
				body: JSON.stringify({ older_than: olderThan }),
			});
			if (!response.ok) {
				const text = await response.text();
				// Bare status + body: the caller's toast already prefixes "Failed to
				// delete requests", so an extra "Failed to purge logs" would read as
				// if app logs were involved.
				throw new Error(`${response.status} ${text}`);
			}
		},
		cursor: async (params: {
			cursor?: string;
			direction?: "after" | "before";
			limit?: number;
			model_id?: string;
			provider_id?: string;
			status_code?: string;
			endpoint_type?: string;
			from?: string;
			to?: string;
			sort_dir?: string;
			/** Admin-only filter: scope to keys owned by this user. */
			owner_user_id?: string;
		}): Promise<LogsCursorResponse> => {
			return fetchJSON<LogsCursorResponse>(
				buildUrl("/api/logs/cursor", {
					cursor: params.cursor,
					direction: params.direction,
					limit: params.limit,
					model_id: params.model_id,
					provider_id: params.provider_id,
					status_code: params.status_code,
					endpoint_type: params.endpoint_type,
					from: params.from,
					to: params.to,
					sort_dir: params.sort_dir,
					owner_user_id: params.owner_user_id,
				}),
				{ headers: getAuthHeaders() },
				"Failed to fetch logs (cursor)",
			);
		},
	},

	appLogs: {
		list: async (params?: {
			limit?: number;
			after?: string;
		}): Promise<AppLogEntry[]> => {
			return fetchJSON<AppLogEntry[]>(
				buildUrl("/api/logs/app", {
					limit: params?.limit,
					after: params?.after,
				}),
				{ headers: getAuthHeaders() },
				"Failed to fetch app logs",
			);
		},
		purge: async (olderThan?: string): Promise<{ deleted: number }> => {
			return fetchJSON<{ deleted: number }>(
				`${API_BASE}/api/logs/app`,
				{
					method: "DELETE",
					headers: getAuthHeaders(),
					// Omit the body for a clear-all request so the backend default
					// ("all") still applies; send a token to prune a range.
					...(olderThan
						? { body: JSON.stringify({ older_than: olderThan }) }
						: {}),
				},
				"Failed to purge app logs",
			);
		},
		history: async (params?: {
			level?: string;
			source?: string;
			search?: string;
			from?: string;
			to?: string;
			page?: number;
			per_page?: number;
			sort_by?: string;
			sort_dir?: string;
		}): Promise<{
			entries: AppLogEntry[];
			total: number;
			page: number;
			per_page: number;
			level_counts?: Record<string, number>;
			source_counts?: Record<string, number>;
		}> => {
			return fetchJSON<{
				entries: AppLogEntry[];
				total: number;
				page: number;
				per_page: number;
				level_counts?: Record<string, number>;
				source_counts?: Record<string, number>;
			}>(
				buildUrl("/api/logs/app", {
					history: "true",
					level: params?.level,
					source: params?.source,
					search: params?.search,
					from: params?.from,
					to: params?.to,
					page: params?.page,
					per_page: params?.per_page,
					sort_by: params?.sort_by,
					sort_dir: params?.sort_dir,
				}),
				{ headers: getAuthHeaders() },
				"Failed to fetch app log history",
			);
		},
		cursor: async (params?: {
			cursor?: string;
			direction?: "after" | "before";
			limit?: number;
			level?: string;
			source?: string;
			search?: string;
			from?: string;
			to?: string;
			sort_dir?: string;
		}): Promise<AppLogsCursorResponse> => {
			return fetchJSON<AppLogsCursorResponse>(
				buildUrl("/api/logs/app/cursor", {
					cursor: params?.cursor,
					direction: params?.direction,
					limit: params?.limit,
					level: params?.level,
					source: params?.source,
					search: params?.search,
					from: params?.from,
					to: params?.to,
					sort_dir: params?.sort_dir,
				}),
				{ headers: getAuthHeaders() },
				"Failed to fetch app logs (cursor)",
			);
		},
	},

	stats: {
		get: async (opts?: {
			period?: string;
			excludeDeleted?: boolean;
			metric?: "requests" | "tokens";
			includeLatency?: boolean;
			/** Admin-only filter: scope to keys owned by this user. */
			ownerUserID?: string;
		}): Promise<Stats> => {
			return fetchJSON<Stats>(
				buildUrl("/api/stats", {
					period: opts?.period,
					exclude_deleted: opts?.excludeDeleted ? "true" : undefined,
					metric: opts?.metric,
					include_latency: opts?.includeLatency ? "true" : undefined,
					owner_user_id: opts?.ownerUserID,
				}),
				{ headers: getAuthHeaders() },
				"Failed to fetch stats",
			);
		},
		getTimeSeries: async (opts?: {
			period?: string;
			excludeDeleted?: boolean;
			/** Admin-only filter: scope to keys owned by this user. */
			ownerUserID?: string;
		}): Promise<TimeSeriesStats> => {
			return fetchJSON<TimeSeriesStats>(
				buildUrl("/api/stats/timeseries", {
					period: opts?.period,
					exclude_deleted: opts?.excludeDeleted ? "true" : undefined,
					owner_user_id: opts?.ownerUserID,
				}),
				{ headers: getAuthHeaders() },
				"Failed to fetch time-series stats",
			);
		},
		getProviderDistribution: async (opts?: {
			period?: string;
			metric?: string;
			excludeDeleted?: boolean;
			/** Admin-only filter: scope to keys owned by this user. */
			ownerUserID?: string;
		}): Promise<ProviderDistributionStats> => {
			return fetchJSON<ProviderDistributionStats>(
				buildUrl("/api/stats/provider-distribution", {
					period: opts?.period,
					metric: opts?.metric,
					exclude_deleted: opts?.excludeDeleted ? "true" : undefined,
					owner_user_id: opts?.ownerUserID,
				}),
				{ headers: getAuthHeaders() },
				"Failed to fetch provider distribution",
			);
		},
	},

	settings: {
		get: async (): Promise<Record<string, string>> => {
			return fetchJSON<Record<string, string>>(
				`${API_BASE}/api/settings`,
				{
					headers: getAuthHeaders(),
				},
				"Failed to fetch settings",
			);
		},
		update: async (
			settings: Record<string, string>,
		): Promise<Record<string, string>> => {
			return fetchJSON<Record<string, string>>(
				`${API_BASE}/api/settings`,
				{
					method: "PUT",
					headers: getAuthHeaders(),
					body: JSON.stringify(settings),
				},
				"Failed to update settings",
			);
		},
		reset: async (keys: string[] = []): Promise<Record<string, string>> => {
			return fetchJSON<Record<string, string>>(
				`${API_BASE}/api/settings`,
				{
					method: "DELETE",
					headers: getAuthHeaders(),
					body: JSON.stringify({ keys }),
				},
				"Failed to reset settings",
			);
		},
	},

	alert: {
		getEvents: async (): Promise<AlertEventDef[]> => {
			return fetchJSON<AlertEventDef[]>(
				`${API_BASE}/api/alert/events`,
				{ headers: getAuthHeaders() },
				"Failed to fetch alert events",
			);
		},
		// With no body, tests the saved configuration (the card's Send test) and
		// sends no request body at all, so no Content-Type header rides along
		// either. The setup wizard passes an explicit {api_url, targets} to test
		// values before either is saved; either field may be omitted to fall back
		// to the saved one.
		test: async (body?: {
			api_url?: string;
			targets?: string[];
		}): Promise<{ ok: boolean }> => {
			const headers = getAuthHeaders();
			if (!body) delete headers["Content-Type"];
			return fetchJSON<{ ok: boolean }>(
				`${API_BASE}/api/alert/test`,
				{
					method: "POST",
					headers,
					...(body ? { body: JSON.stringify(body) } : {}),
				},
				"Test notification failed",
			);
		},
		status: async (): Promise<AlertStatus> => {
			return fetchJSON<AlertStatus>(
				`${API_BASE}/api/alert/status`,
				{ headers: getAuthHeaders() },
				"Failed to fetch alert status",
			);
		},
		// Checks the reachability of an apprise-api URL the operator typed but has
		// not saved yet; the setup wizard gates its first step on this.
		probe: async (apiUrl: string): Promise<AlertStatus> => {
			return fetchJSON<AlertStatus>(
				`${API_BASE}/api/alert/probe`,
				{
					method: "POST",
					headers: getAuthHeaders(),
					body: JSON.stringify({ api_url: apiUrl }),
				},
				"Failed to probe alert URL",
			);
		},
		// The saved destinations, decrypted for the admin UI's readable list.
		targets: async (): Promise<AlertTargets> => {
			return fetchJSON<AlertTargets>(
				`${API_BASE}/api/alert/targets`,
				{ headers: getAuthHeaders() },
				"Failed to fetch alert targets",
			);
		},
	},

	version: {
		getLatest: async (options?: RequestInit): Promise<{ tag_name: string }> => {
			return fetchJSON<{ tag_name: string }>(
				`${API_BASE}/api/version/latest`,
				{ headers: getAuthHeaders(), ...options },
				"Failed to fetch latest version",
			);
		},
	},

	virtualKeys: {
		list: async (): Promise<VirtualKey[]> => {
			return fetchJSON<VirtualKey[]>(
				`${API_BASE}/api/virtual-keys`,
				{
					headers: getAuthHeaders(),
				},
				"Failed to fetch virtual keys",
			);
		},
		create: async (
			name: string,
			rate_limit_rps?: number | null,
			rate_limit_burst?: number | null,
			rate_limit_tpm?: number | null,
			allowed_providers?: string[] | null,
			strip_reasoning?: boolean,
			owner_user_id?: string | null,
		): Promise<VirtualKey> => {
			return fetchJSON<VirtualKey>(
				`${API_BASE}/api/virtual-keys`,
				{
					method: "POST",
					headers: getAuthHeaders(),
					body: JSON.stringify({
						name,
						rate_limit_rps,
						rate_limit_burst,
						rate_limit_tpm,
						allowed_providers,
						strip_reasoning,
						owner_user_id,
					}),
				},
				"Failed to create virtual key",
			);
		},
		get: async (id: string): Promise<VirtualKey> => {
			return fetchJSON<VirtualKey>(
				`${API_BASE}/api/virtual-keys/${id}`,
				{
					headers: getAuthHeaders(),
				},
				"Failed to fetch virtual key",
			);
		},
		update: async (
			id: string,
			data: {
				name: string;
				rate_limit_rps?: number | null;
				rate_limit_burst?: number | null;
				rate_limit_tpm?: number | null;
				allowed_providers?: string[] | null;
				strip_reasoning?: boolean;
				// Omit to preserve the current owner; null clears it (admin only).
				owner_user_id?: string | null;
			},
		): Promise<VirtualKey> => {
			return fetchJSON<VirtualKey>(
				`${API_BASE}/api/virtual-keys/${id}`,
				{
					method: "PUT",
					headers: getAuthHeaders(),
					body: JSON.stringify(data),
				},
				"Failed to update virtual key",
			);
		},
		delete: async (id: string): Promise<void> => {
			const response = await fetch(`${API_BASE}/api/virtual-keys/${id}`, {
				method: "DELETE",
				headers: getAuthHeaders(),
			});
			if (!response.ok) {
				throw new Error("Failed to delete virtual key");
			}
		},
	},

	system: {
		get: async (): Promise<SystemStats> => {
			const now = new Date();
			const midnight = new Date(
				now.getFullYear(),
				now.getMonth(),
				now.getDate(),
			);
			const since = midnight.toISOString();
			return fetchJSON<SystemStats>(
				`${API_BASE}/api/system?since=${encodeURIComponent(since)}`,
				{
					headers: getAuthHeaders(),
				},
				"Failed to fetch system stats",
			);
		},
	},

	chat: {
		completions: async (body: {
			model: string;
			stream: boolean;
			messages: Array<{ role: string; content: string }>;
			temperature?: number;
			max_tokens?: number;
			top_p?: number;
			min_p?: number;
			top_k?: number;
			frequency_penalty?: number;
			presence_penalty?: number;
		}): Promise<Response> => {
			return fetchOK(
				`${API_BASE}/api/chat/completions`,
				{
					method: "POST",
					headers: getAuthHeaders(),
					body: JSON.stringify(body),
				},
				"Chat failed",
			);
		},

		chat: async (body: {
			model: string;
			stream: boolean;
			messages: Array<{ role: string; content: string }>;
			temperature?: number;
			max_tokens?: number;
			top_p?: number;
			min_p?: number;
			top_k?: number;
			frequency_penalty?: number;
			presence_penalty?: number;
			signal?: AbortSignal;
		}): Promise<Response> => {
			return fetchOK(
				`${API_BASE}/api/chat/chat`,
				{
					method: "POST",
					headers: getAuthHeaders(),
					body: JSON.stringify(body),
					...(body.signal ? { signal: body.signal } : {}),
				},
				"Chat failed",
			);
		},

		arena: async (body: {
			model: string;
			stream: boolean;
			messages: Array<{ role: string; content: string }>;
			temperature?: number;
			max_tokens?: number;
			top_p?: number;
			min_p?: number;
			top_k?: number;
			frequency_penalty?: number;
			presence_penalty?: number;
			signal?: AbortSignal;
		}): Promise<Response> => {
			return fetchOK(
				`${API_BASE}/api/chat/arena`,
				{
					method: "POST",
					headers: getAuthHeaders(),
					body: JSON.stringify(body),
					...(body.signal ? { signal: body.signal } : {}),
				},
				"Arena failed",
			);
		},
	},

	failoverGroups: {
		list: async (): Promise<FailoverListResponse> => {
			return fetchJSON<FailoverListResponse>(
				`${API_BASE}/api/failover-groups`,
				{
					headers: getAuthHeaders(),
				},
				"Failed to fetch failover groups",
			);
		},
		get: async (id: string): Promise<FailoverGroup> => {
			return fetchJSON<FailoverGroup>(
				`${API_BASE}/api/failover-groups/${id}`,
				{
					headers: getAuthHeaders(),
				},
				"Failed to fetch failover group",
			);
		},
		create: async (
			data: CreateFailoverGroupRequest,
		): Promise<FailoverGroup> => {
			return fetchJSON<FailoverGroup>(
				`${API_BASE}/api/failover-groups`,
				{
					method: "POST",
					headers: getAuthHeaders(),
					body: JSON.stringify(data),
				},
				"Failed to create failover group",
			);
		},
		update: async (
			id: string,
			data: UpdateFailoverGroupRequest,
		): Promise<FailoverGroup> => {
			return fetchJSON<FailoverGroup>(
				`${API_BASE}/api/failover-groups/${id}`,
				{
					method: "PUT",
					headers: getAuthHeaders(),
					body: JSON.stringify(data),
				},
				"Failed to update failover group",
			);
		},
		delete: async (id: string): Promise<void> => {
			const response = await fetch(`${API_BASE}/api/failover-groups/${id}`, {
				method: "DELETE",
				headers: getAuthHeaders(),
			});
			if (!response.ok) {
				throw new Error("Failed to delete failover group");
			}
		},
		sync: async (): Promise<SyncResult> => {
			return fetchJSON<SyncResult>(
				`${API_BASE}/api/failover-groups/sync`,
				{
					method: "POST",
					headers: getAuthHeaders(),
				},
				"Failed to sync failover groups",
			);
		},
		candidates: async (): Promise<CandidateModel[]> => {
			return fetchJSON<CandidateModel[]>(
				`${API_BASE}/api/failover-groups/candidates`,
				{
					headers: getAuthHeaders(),
				},
				"Failed to fetch candidates",
			);
		},
		circuitBreakerStatus: async (
			detail = false,
		): Promise<CircuitBreakerStatus> => {
			const url = detail
				? `${API_BASE}/api/failover-groups/circuit-breaker-status?detail=1`
				: `${API_BASE}/api/failover-groups/circuit-breaker-status`;
			return fetchJSON<CircuitBreakerStatus>(
				url,
				{
					headers: getAuthHeaders(),
				},
				"Failed to fetch circuit breaker status",
			);
		},
		// Forces one provider's circuit closed so it returns to rotation without
		// waiting out the cooldown. Not blocked on a managed fleet member: a
		// circuit is local runtime health, not synced config.
		resetCircuitBreaker: async (
			providerId: string,
		): Promise<CircuitBreakerResetResult> => {
			return fetchJSON<CircuitBreakerResetResult>(
				`${API_BASE}/api/failover-groups/circuit-breaker/${encodeURIComponent(providerId)}/reset`,
				{
					method: "POST",
					headers: getAuthHeaders(),
				},
				"Failed to reset circuit breaker",
			);
		},
	},

	backups: {
		list: async (): Promise<BackupEntry[]> => {
			return fetchJSON<BackupEntry[]>(
				`${API_BASE}/api/backups`,
				{
					headers: getAuthHeaders(),
				},
				"Failed to fetch backups",
			);
		},
		create: async (): Promise<BackupEntry> => {
			return fetchJSON<BackupEntry>(
				`${API_BASE}/api/backups`,
				{
					method: "POST",
					headers: getAuthHeaders(),
				},
				"Failed to create backup",
			);
		},
		downloadUrl: (filename: string): string => {
			return `${API_BASE}/api/backups/${encodeURIComponent(filename)}`;
		},
		/** The backup's signature sidecar contents, for pasting into the restore
		 *  form. 404 (thrown) for an unsigned backup. */
		signature: async (filename: string): Promise<{ signature: string }> => {
			return fetchJSON<{ signature: string }>(
				`${API_BASE}/api/backups/${encodeURIComponent(filename)}/signature`,
				{ headers: getAuthHeaders() },
				"Failed to fetch backup signature",
			);
		},
		delete: async (filename: string): Promise<void> => {
			const response = await fetch(
				`${API_BASE}/api/backups/${encodeURIComponent(filename)}`,
				{
					method: "DELETE",
					headers: getAuthHeaders(),
				},
			);
			if (!response.ok) {
				throw new Error("Failed to delete backup");
			}
		},
		restore: async (
			file: File,
			adminToken: string,
			signature = "",
		): Promise<{ migration_count: number; known_count: number }> => {
			const formData = new FormData();
			formData.append("dump", file);
			formData.append("admin_token", adminToken);
			// The dump's .sig sidecar contents, when the operator has them. The
			// server rejects a mismatch outright and records a restore without one
			// as unverified. It treats a blank value as absent itself; omitting the
			// field here just keeps the request honest about what was supplied.
			if (signature.trim()) {
				formData.append("signature", signature.trim());
			}

			// Must not set Content-Type: the browser needs to auto-set
			// multipart/form-data with the correct boundary for FormData. The
			// session rides in the cookie; the CSRF token authorizes this write.
			const csrf = getCsrfToken();
			const response = await fetch(`${API_BASE}/api/backups/restore`, {
				method: "POST",
				credentials: "same-origin",
				headers: csrf ? { "X-CSRF-Token": csrf } : {},
				body: formData,
			});
			if (!response.ok) {
				const text = await response.text();
				throw new Error(`Restore failed: ${response.status} ${text}`);
			}
			return response.json();
		},
		prunePreview: async (): Promise<BackupClassification> => {
			return fetchJSON<BackupClassification>(
				`${API_BASE}/api/backups/prune-preview`,
				{
					method: "POST",
					headers: getAuthHeaders(),
				},
				"Failed to preview backup pruning",
			);
		},
		prune: async (): Promise<BackupClassification> => {
			return fetchJSON<BackupClassification>(
				`${API_BASE}/api/backups/prune`,
				{
					method: "POST",
					headers: getAuthHeaders(),
				},
				"Failed to prune backups",
			);
		},
	},
	webauthn: {
		available: async (): Promise<{
			enabled: boolean;
			has_credentials: boolean;
		}> => {
			return fetchJSON(`${API_BASE}/api/webauthn/available`);
		},
		registerStart: async (): Promise<{
			session_id: string;
			options: Record<string, unknown>;
		}> => {
			return fetchJSON(`${API_BASE}/api/webauthn/register/start`, {
				method: "POST",
				headers: getAuthHeaders(),
			});
		},
		registerFinish: async (
			sessionId: string,
			credential: unknown,
		): Promise<void> => {
			await fetchOK(
				`${API_BASE}/api/webauthn/register/finish`,
				{
					method: "POST",
					headers: getAuthHeaders(),
					body: JSON.stringify({ session_id: sessionId, credential }),
				},
				"Passkey registration failed",
			);
		},
		loginStart: async (): Promise<{
			session_id: string;
			options: Record<string, unknown>;
		}> => {
			return fetchJSON(`${API_BASE}/api/webauthn/login/start`, {
				method: "POST",
				headers: { "Content-Type": "application/json" },
			});
		},
		loginFinish: async (
			sessionId: string,
			credential: unknown,
		): Promise<{ token: string }> => {
			return fetchJSON(
				`${API_BASE}/api/webauthn/login/finish`,
				{
					method: "POST",
					headers: { "Content-Type": "application/json" },
					body: JSON.stringify({ session_id: sessionId, credential }),
				},
				"Passkey login failed",
			);
		},
		listCredentials: async (): Promise<
			import("./types").WebAuthnCredential[]
		> => {
			return fetchJSON<import("./types").WebAuthnCredential[]>(
				`${API_BASE}/api/webauthn/credentials`,
				{ headers: getAuthHeaders() },
			);
		},
		deleteCredential: async (id: string): Promise<void> => {
			await fetchOK(
				`${API_BASE}/api/webauthn/credentials/${encodeURIComponent(id)}`,
				{
					method: "DELETE",
					headers: getAuthHeaders(),
				},
				"Failed to delete passkey",
			);
		},
		renameCredential: async (id: string, name: string): Promise<void> => {
			await fetchOK(
				`${API_BASE}/api/webauthn/credentials/${encodeURIComponent(id)}`,
				{
					method: "PATCH",
					headers: { ...getAuthHeaders(), "Content-Type": "application/json" },
					body: JSON.stringify({ name }),
				},
				"Failed to rename passkey",
			);
		},
	},
	totp: {
		status: async (): Promise<TotpStatus> =>
			fetchJSON<TotpStatus>(`${API_BASE}/api/totp/status`),
		info: async (): Promise<TotpInfo> =>
			fetchJSON<TotpInfo>(`${API_BASE}/api/totp/info`, {
				headers: getAuthHeaders(),
			}),
		enrollStart: async (): Promise<TotpEnrollStart> =>
			fetchJSON<TotpEnrollStart>(
				`${API_BASE}/api/totp/enroll/start`,
				{ method: "POST", headers: getAuthHeaders() },
				"TOTP enrollment failed",
			),
		enrollVerify: async (code: string): Promise<TotpEnrollVerify> =>
			fetchJSON<TotpEnrollVerify>(
				`${API_BASE}/api/totp/enroll/verify`,
				{
					method: "POST",
					headers: getAuthHeaders(),
					body: JSON.stringify({ code }),
				},
				"TOTP verification failed",
			),
		disable: async (code: string): Promise<void> => {
			await fetchOK(
				`${API_BASE}/api/totp/disable`,
				{
					method: "POST",
					headers: getAuthHeaders(),
					body: JSON.stringify({ code }),
				},
				"TOTP disable failed",
			);
		},
		login: async (token: string, code: string): Promise<TotpLoginResponse> =>
			fetchJSON<TotpLoginResponse>(
				`${API_BASE}/api/totp/login`,
				{
					method: "POST",
					headers: { "Content-Type": "application/json" },
					body: JSON.stringify({ token, code }),
				},
				"TOTP login failed",
			),
	},
	// Unauthenticated: read on the login screen and in settings. The actual
	// login is a full-page redirect to /api/auth/oidc/start, not an XHR.
	oidc: {
		status: async (): Promise<OidcStatus> =>
			fetchJSON<OidcStatus>(`${API_BASE}/api/auth/oidc/status`),
	},
	// Unauthenticated: read on the login screen and in settings. The actual
	// login is a full-page redirect to /api/auth/github/start, not an XHR.
	github: {
		status: async (): Promise<GithubStatus> =>
			fetchJSON<GithubStatus>(`${API_BASE}/api/auth/github/status`),
	},
	// Multi-user auth: the status probe and login exchange are unauthenticated
	// (login-screen surface); me reports the caller's resolved identity.
	auth: {
		status: async (): Promise<AuthStatus> =>
			fetchJSON<AuthStatus>(`${API_BASE}/api/auth/status`),
		login: async (
			username: string,
			password: string,
			code?: string,
		): Promise<{ token: string }> =>
			fetchJSON<{ token: string }>(
				`${API_BASE}/api/auth/login`,
				{
					method: "POST",
					headers: { "Content-Type": "application/json" },
					// code rides along only when the account has TOTP enabled (the
					// 401 {"totp_required": true} answer tells the UI to ask for it).
					body: JSON.stringify(
						code ? { username, password, code } : { username, password },
					),
				},
				"Login failed",
			),
		// Admin-token bootstrap: exchanges the env admin token for a session
		// cookie pair. Returns 400 when the admin account has TOTP enabled (the
		// caller must use the TOTP login flow instead), 401 on a bad token.
		adminExchange: async (adminToken: string): Promise<{ success: boolean }> =>
			fetchJSON<{ success: boolean }>(
				`${API_BASE}/api/auth/admin-exchange`,
				{
					method: "POST",
					headers: { "Content-Type": "application/json" },
					body: JSON.stringify({ admin_token: adminToken }),
				},
				"Login failed",
			),
		// Always-mounted logout: revokes whatever session the caller presents
		// (passkey OR TOTP session token) and clears both auth cookies
		// server-side. Works with or without passkeys configured, unlike the
		// passkey-gated /webauthn/logout.
		logout: async (): Promise<void> => {
			await fetchOK(
				`${API_BASE}/api/auth/logout`,
				{
					method: "POST",
					headers: getAuthHeaders(),
				},
				"Failed to logout",
			);
		},
		me: async (): Promise<Me> =>
			fetchJSON<Me>(`${API_BASE}/api/auth/me`, {
				headers: getAuthHeaders(),
			}),
		// Signs the caller's other sessions out, keeping this one, and reports
		// how many were ended so the UI can say so.
		revokeOtherSessions: async (): Promise<{ revoked: number }> =>
			fetchJSON<{ revoked: number }>(
				`${API_BASE}/api/auth/sessions/revoke-others`,
				{ method: "POST", headers: getAuthHeaders() },
				"Could not sign out other sessions",
			),
		// The caller's own live sessions, for the Settings active-sessions list.
		listSessions: async (): Promise<{ sessions: AuthSession[] }> =>
			fetchJSON<{ sessions: AuthSession[] }>(
				`${API_BASE}/api/auth/sessions`,
				{ headers: getAuthHeaders() },
				"Could not load sessions",
			),
		// Signs one of the caller's other sessions out by row id.
		revokeSession: async (id: string): Promise<void> => {
			await fetchOK(
				`${API_BASE}/api/auth/sessions/${id}`,
				{ method: "DELETE", headers: getAuthHeaders() },
				"Could not sign out the session",
			);
		},
	},
	// Self-service per-user TOTP (users-row identities manage their own 2FA;
	// the env-token admin uses api.totp instead).
	userTotp: {
		status: async (): Promise<UserTotpStatus> =>
			fetchJSON<UserTotpStatus>(`${API_BASE}/api/auth/totp/status`, {
				headers: getAuthHeaders(),
			}),
		enrollStart: async (): Promise<TotpEnrollStart> =>
			fetchJSON<TotpEnrollStart>(
				`${API_BASE}/api/auth/totp/enroll/start`,
				{ method: "POST", headers: getAuthHeaders() },
				"TOTP enrollment failed",
			),
		enrollVerify: async (code: string): Promise<{ recovery_codes: string[] }> =>
			fetchJSON<{ recovery_codes: string[] }>(
				`${API_BASE}/api/auth/totp/enroll/verify`,
				{
					method: "POST",
					headers: getAuthHeaders(),
					body: JSON.stringify({ code }),
				},
				"TOTP verification failed",
			),
		disable: async (code: string): Promise<void> => {
			await fetchOK(
				`${API_BASE}/api/auth/totp/disable`,
				{
					method: "POST",
					headers: getAuthHeaders(),
					body: JSON.stringify({ code }),
				},
				"TOTP disable failed",
			);
		},
		// Rotates the caller's own password; the server revokes every session
		// of the account on success, this one included.
		changePassword: async (
			currentPassword: string,
			newPassword: string,
		): Promise<void> => {
			await fetchOK(
				`${API_BASE}/api/auth/password`,
				{
					method: "POST",
					headers: getAuthHeaders(),
					body: JSON.stringify({
						current_password: currentPassword,
						new_password: newPassword,
					}),
				},
				"Password change failed",
			);
		},
	},
	// Admin-only audit trail of admin actions.
	audit: {
		list: async (params?: {
			cursor?: string;
			limit?: number;
			offset?: number;
			actor?: string;
			method?: string;
			from?: string;
			to?: string;
		}): Promise<AuditListResponse> =>
			fetchJSON<AuditListResponse>(
				buildUrl("/api/audit", {
					cursor: params?.cursor,
					limit: params?.limit,
					offset: params?.offset,
					actor: params?.actor,
					method: params?.method,
					from: params?.from,
					to: params?.to,
				}),
				{ headers: getAuthHeaders() },
				"Failed to fetch audit entries",
			),
		purge: async (olderThan: string): Promise<void> => {
			await fetchOK(
				`${API_BASE}/api/audit/purge`,
				{
					method: "DELETE",
					headers: getAuthHeaders(),
					body: JSON.stringify({ older_than: olderThan }),
				},
				"Failed to purge audit entries",
			);
		},
	},
	// Admin-only user management.
	users: {
		list: async (): Promise<DashboardUser[]> =>
			fetchJSON<DashboardUser[]>(`${API_BASE}/api/users`, {
				headers: getAuthHeaders(),
			}),
		grants: async (): Promise<{ grants: string[] }> =>
			fetchJSON<{ grants: string[] }>(`${API_BASE}/api/users/grants`, {
				headers: getAuthHeaders(),
			}),
		create: async (req: UserUpsertRequest): Promise<DashboardUser> =>
			fetchJSON<DashboardUser>(
				`${API_BASE}/api/users`,
				{
					method: "POST",
					headers: getAuthHeaders(),
					body: JSON.stringify(req),
				},
				"Failed to create user",
			),
		update: async (
			id: string,
			req: UserUpsertRequest,
		): Promise<DashboardUser> =>
			fetchJSON<DashboardUser>(
				`${API_BASE}/api/users/${id}`,
				{
					method: "PUT",
					headers: getAuthHeaders(),
					body: JSON.stringify(req),
				},
				"Failed to update user",
			),
		setPassword: async (id: string, password: string): Promise<void> => {
			await fetchJSON<{ ok: boolean }>(
				`${API_BASE}/api/users/${id}/password`,
				{
					method: "POST",
					headers: getAuthHeaders(),
					body: JSON.stringify({ password }),
				},
				"Failed to set password",
			);
		},
		remove: async (id: string): Promise<void> => {
			await fetchOK(
				`${API_BASE}/api/users/${id}`,
				{ method: "DELETE", headers: getAuthHeaders() },
				"Failed to delete user",
			);
		},
		resetTotp: async (id: string): Promise<void> => {
			await fetchOK(
				`${API_BASE}/api/users/${id}/totp/reset`,
				{ method: "POST", headers: getAuthHeaders() },
				"Failed to reset TOTP",
			);
		},
	},
};
