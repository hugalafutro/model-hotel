import type {
	CandidateModel,
	CreateFailoverGroupRequest,
	CreateProviderRequest,
	DeepSeekBalance,
	FailoverGroup,
	FailoverListResponse,
	LogsResponse,
	Model,
	NanoGPTUsage,
	Provider,
	ProviderDistributionStats,
	Stats,
	SyncResult,
	SystemStats,
	TimeSeriesStats,
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

async function fetchOK(
	url: string,
	options?: RequestInit,
	errorPrefix = "Request failed",
): Promise<Response> {
	const response = await fetch(url, options);
	if (!response.ok) {
		const text = await response.text();
		throw new Error(`${errorPrefix}: ${response.status} ${text}`);
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

function buildQueryString(
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

function buildUrl(
	path: string,
	params?: Record<string, string | number | boolean | undefined>,
): string {
	if (!params) return `${API_BASE}${path}`;
	const qs = buildQueryString(params);
	return qs ? `${API_BASE}${path}?${qs}` : `${API_BASE}${path}`;
}

// ── API ─────────────────────────────────────────────────────────────

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
			if (!response.ok && response.status !== 204) {
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
		discover: async (id: string): Promise<{ discovered: number }> => {
			return fetchJSON<{ discovered: number }>(
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
			results: {
				provider_name: string;
				discovered: number;
				error?: string;
			}[];
		}> => {
			return fetchJSON<{
				succeeded: number;
				failed: number;
				discovered: number;
				results: {
					provider_name: string;
					discovered: number;
					error?: string;
				}[];
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
		): Promise<{
			success: boolean;
			ttft_ms: number;
			duration_ms: number;
			response: string;
			error?: string;
		}> => {
			return fetchJSON<{
				success: boolean;
				ttft_ms: number;
				duration_ms: number;
				response: string;
				error?: string;
			}>(
				`${API_BASE}/api/models/${id}/test`,
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
			if (!response.ok && response.status !== 204) {
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
					from: params.from,
					to: params.to,
					sort_by: params.sort_by,
					sort_dir: params.sort_dir,
				}),
				{ headers: getAuthHeaders() },
				"Failed to fetch logs",
			);
		},
		purge: async (olderThan: string): Promise<void> => {
			const response = await fetch(`${API_BASE}/api/logs/purge`, {
				method: "DELETE",
				headers: getAuthHeaders(),
				body: JSON.stringify({ older_than: olderThan }),
			});
			if (!response.ok && response.status !== 204) {
				const text = await response.text();
				throw new Error(`Failed to purge logs: ${response.status} ${text}`);
			}
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
	},

	stats: {
		get: async (opts?: {
			period?: string;
			excludeDeleted?: boolean;
			metric?: "requests" | "tokens";
		}): Promise<Stats> => {
			return fetchJSON<Stats>(
				buildUrl("/api/stats", {
					period: opts?.period,
					exclude_deleted: opts?.excludeDeleted ? "true" : undefined,
					metric: opts?.metric,
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
		create: async (name: string): Promise<VirtualKey> => {
			return fetchJSON<VirtualKey>(
				`${API_BASE}/api/virtual-keys`,
				{
					method: "POST",
					headers: getAuthHeaders(),
					body: JSON.stringify({ name }),
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
		delete: async (id: string): Promise<void> => {
			const response = await fetch(`${API_BASE}/api/virtual-keys/${id}`, {
				method: "DELETE",
				headers: getAuthHeaders(),
			});
			if (!response.ok && response.status !== 204) {
				throw new Error("Failed to delete virtual key");
			}
		},
	},

	system: {
		get: async (): Promise<SystemStats> => {
			return fetchJSON<SystemStats>(
				`${API_BASE}/api/system`,
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
			if (!response.ok && response.status !== 204) {
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
	},
};
