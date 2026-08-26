import { API_BASE, fetchJSON, getAuthHeaders } from "../http";
import type {
	CandidateModel,
	CircuitBreakerResetResult,
	CircuitBreakerStatus,
	CreateFailoverGroupRequest,
	FailoverGroup,
	FailoverListResponse,
	Model,
	ModelsCursorResponse,
	SyncResult,
	UpdateFailoverGroupRequest,
} from "../types";

export const models = {
	list: async (
		providerId?: string,
		providerEnabled?: boolean,
	): Promise<Model[]> => {
		const sp = new URLSearchParams();
		if (providerId) sp.set("provider_id", providerId);
		if (providerEnabled !== undefined)
			sp.set("provider_enabled", String(providerEnabled));
		const qs = sp.toString();
		const url = qs ? `${API_BASE}/api/models?${qs}` : `${API_BASE}/api/models`;
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
		/** Filter on the owning provider's enabled flag; undefined = any. */
		provider_enabled?: boolean;
		/** Filter on the model's own enabled flag; undefined = any. */
		enabled?: boolean;
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
		if (params.provider_enabled !== undefined)
			sp.set("provider_enabled", String(params.provider_enabled));
		if (params.enabled !== undefined) sp.set("enabled", String(params.enabled));
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
};
