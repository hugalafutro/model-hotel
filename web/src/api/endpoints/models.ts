import {
	API_BASE,
	buildUrl,
	fetchJSON,
	fetchOK,
	getAuthHeaders,
} from "../http";
import type {
	CandidateModel,
	CircuitBreakerResetResult,
	CircuitBreakerStatus,
	CreateFailoverGroupRequest,
	FailoverGroup,
	FailoverListResponse,
	Model,
	ModelsCursorResponse,
	ModelTestResult,
	SyncResult,
	UpdateFailoverGroupRequest,
} from "../types";

export const models = {
	list: async (
		providerId?: string,
		providerEnabled?: boolean,
	): Promise<Model[]> => {
		return fetchJSON<Model[]>(
			buildUrl("/api/models", {
				provider_id: providerId || undefined,
				provider_enabled: providerEnabled,
			}),
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
		/** Filter on the owning provider's enabled flag; undefined = any. */
		provider_enabled?: boolean;
		/** Filter on the model's own enabled flag; undefined = any. */
		enabled?: boolean;
	}): Promise<ModelsCursorResponse> => {
		// buildUrl drops undefined values; the empty-string filters are mapped to
		// undefined so a cleared search box does not become "search=".
		return fetchJSON<ModelsCursorResponse>(
			buildUrl("/api/models/cursor", {
				...params,
				cursor: params.cursor || undefined,
				sort_by: params.sort_by || undefined,
				sort_dir: params.sort_dir || undefined,
				provider_id: params.provider_id || undefined,
				search: params.search || undefined,
				capabilities: params.capabilities || undefined,
				outputs: params.outputs || undefined,
			}),
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
	): Promise<ModelTestResult> => {
		return fetchJSON<ModelTestResult>(
			buildUrl(`/api/models/${id}/test`, {
				allow_disabled: allowDisabled || undefined,
			}),
			{
				method: "POST",
				headers: getAuthHeaders(),
			},
			"Test failed",
		);
	},
	delete: async (id: string): Promise<void> => {
		await fetchOK(
			`${API_BASE}/api/models/${id}`,
			{ method: "DELETE", headers: getAuthHeaders() },
			"Failed to delete model",
		);
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
};

export const failoverGroups = {
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
	create: async (data: CreateFailoverGroupRequest): Promise<FailoverGroup> => {
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
		await fetchOK(
			`${API_BASE}/api/failover-groups/${id}`,
			{ method: "DELETE", headers: getAuthHeaders() },
			"Failed to delete failover group",
		);
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
		return fetchJSON<CircuitBreakerStatus>(
			buildUrl("/api/failover-groups/circuit-breaker-status", {
				detail: detail ? 1 : undefined,
			}),
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
};
