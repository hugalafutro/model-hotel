import {
	API_BASE,
	buildUrl,
	fetchJSON,
	fetchOK,
	getAuthHeaders,
} from "../http";
import type {
	AppLogEntry,
	AppLogsCursorResponse,
	AuditListResponse,
	LogEntry,
	LogsCursorResponse,
	LogsResponse,
	ProviderDistributionStats,
	Stats,
	TimeSeriesStats,
} from "../types";

export const logs = {
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
			/** Filter to a single virtual key's traffic. */
			virtual_key_id?: string;
			/** Filter to requests from one exact client address. */
			client_ip?: string;
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
				virtual_key_id: params.virtual_key_id,
				client_ip: params.client_ip,
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
		/** Filter to a single virtual key's traffic. */
		virtual_key_id?: string;
		/** Filter to requests from one exact client address. */
		client_ip?: string;
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
				virtual_key_id: params.virtual_key_id,
				client_ip: params.client_ip,
				owner_user_id: params.owner_user_id,
			}),
			{ headers: getAuthHeaders() },
			"Failed to fetch logs (cursor)",
		);
	},
};

export const appLogs = {
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
};

export const stats = {
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
};

// Admin-only audit trail of admin actions.
export const audit = {
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
};
