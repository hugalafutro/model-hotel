import type {
	AlertEventDef,
	AlertStatus,
	AppLogsCursorResponse,
	BackupClassification,
	BackupEntry,
	CandidateModel,
	CircuitBreakerStatus,
	CreateFailoverGroupRequest,
	CreateProviderRequest,
	DeepSeekBalance,
	DemoLogin,
	DiscoverAllResult,
	DiscoveryChangesResponse,
	DiscoveryDiff,
	FailoverGroup,
	FailoverListResponse,
	LogEntry,
	LogsCursorResponse,
	LogsResponse,
	Model,
	ModelsCursorResponse,
	NanoGPTUsage,
	NeuralWattQuotaResponse,
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
// message is unchanged, so existing catch blocks keep working.
export class ApiError extends Error {
	readonly status: number;
	constructor(message: string, status: number) {
		super(message);
		this.name = "ApiError";
		this.status = status;
	}
}

async function fetchOK(
	url: string,
	options?: RequestInit,
	errorPrefix = "Request failed",
): Promise<Response> {
	const response = await fetch(url, options);
	if (!response.ok) {
		const text = await response.text();
		throw new ApiError(
			`${errorPrefix}: ${response.status} ${text}`,
			response.status,
		);
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

// Admin token is stored in memory (preferred) with localStorage as a
// persistence fallback so sessions survive page reloads. This is an explicit
// trade-off: the dashboard is same-origin and admin-only, so XSS is the only
// practical attack vector. httpOnly cookies would eliminate the XSS risk but
// require CSRF protection, which adds complexity for a single-user admin
// panel serving an embedded SPA. If you deploy this behind a public-facing
// domain with untrusted users, consider switching to httpOnly cookies.
let adminToken: string | null = null;

export function setAdminToken(token: string) {
	adminToken = token;
}

export function getAdminToken(): string | null {
	return adminToken;
}

export function getAuthHeaders(): Record<string, string> {
	const token = adminToken || localStorage.getItem("adminToken");
	if (!token) {
		throw new Error("Admin token not set");
	}
	return {
		Authorization: `Bearer ${token}`,
		"Content-Type": "application/json",
	};
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
		): Promise<NanoGPTUsage | ZAICodingQuotaResponse> => {
			return fetchJSON<NanoGPTUsage | ZAICodingQuotaResponse>(
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
		// Unseen changes recorded by background (scheduled/startup) discovery,
		// powering the Models nav badge and its review modal.
		changes: async (): Promise<DiscoveryChangesResponse> => {
			return fetchJSON<DiscoveryChangesResponse>(
				`${API_BASE}/api/discovery/changes`,
				{ headers: getAuthHeaders() },
				"Failed to load discovery changes",
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
				output_price_per_million?: number | null;
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
				throw new Error(`Failed to purge logs: ${response.status} ${text}`);
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
		purge: async (): Promise<{ deleted: number }> => {
			return fetchJSON<{ deleted: number }>(
				`${API_BASE}/api/logs/app`,
				{
					method: "DELETE",
					headers: getAuthHeaders(),
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
		}): Promise<Stats> => {
			return fetchJSON<Stats>(
				buildUrl("/api/stats", {
					period: opts?.period,
					exclude_deleted: opts?.excludeDeleted ? "true" : undefined,
					metric: opts?.metric,
					include_latency: opts?.includeLatency ? "true" : undefined,
				}),
				{ headers: getAuthHeaders() },
				"Failed to fetch stats",
			);
		},
		getTimeSeries: async (opts?: {
			period?: string;
			excludeDeleted?: boolean;
		}): Promise<TimeSeriesStats> => {
			return fetchJSON<TimeSeriesStats>(
				buildUrl("/api/stats/timeseries", {
					period: opts?.period,
					exclude_deleted: opts?.excludeDeleted ? "true" : undefined,
				}),
				{ headers: getAuthHeaders() },
				"Failed to fetch time-series stats",
			);
		},
		getProviderDistribution: async (opts?: {
			period?: string;
			metric?: string;
			excludeDeleted?: boolean;
		}): Promise<ProviderDistributionStats> => {
			return fetchJSON<ProviderDistributionStats>(
				buildUrl("/api/stats/provider-distribution", {
					period: opts?.period,
					metric: opts?.metric,
					exclude_deleted: opts?.excludeDeleted ? "true" : undefined,
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
		test: async (): Promise<{ ok: boolean }> => {
			return fetchJSON<{ ok: boolean }>(
				`${API_BASE}/api/alert/test`,
				{ method: "POST", headers: getAuthHeaders() },
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
		): Promise<{ migration_count: number; known_count: number }> => {
			const formData = new FormData();
			formData.append("dump", file);
			formData.append("admin_token", adminToken);

			// Must not set Content-Type: the browser needs to auto-set
			// multipart/form-data with the correct boundary for FormData.
			const token = localStorage.getItem("adminToken") || "";
			const response = await fetch(`${API_BASE}/api/backups/restore`, {
				method: "POST",
				headers: {
					Authorization: `Bearer ${token}`,
				},
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
		available: async (): Promise<{ enabled: boolean }> => {
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
		logout: async (): Promise<void> => {
			await fetchOK(
				`${API_BASE}/api/webauthn/logout`,
				{
					method: "POST",
					headers: getAuthHeaders(),
				},
				"Failed to logout",
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
};
