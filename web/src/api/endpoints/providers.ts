import { API_BASE, fetchJSON, fetchOK, getAuthHeaders } from "../http";
import type {
	CreateProviderRequest,
	DeepSeekBalance,
	DiscoverAllResult,
	DiscoveryChangesResponse,
	DiscoveryDiff,
	DiscoveryStatusResponse,
	KimiCodeQuotaResponse,
	MiniMaxQuotaResponse,
	NanoGPTUsage,
	NeuralWattQuotaResponse,
	OllamaCloudAccount,
	OpenRouterBalance,
	Provider,
	UpdateProviderRequest,
	ZAICodingQuotaResponse,
} from "../types";

export const providers = {
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
		await fetchOK(
			`${API_BASE}/api/providers/${id}`,
			{ method: "DELETE", headers: getAuthHeaders() },
			"Failed to delete provider",
		);
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
};

export const discovery = {
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
	// Dismiss only. There is deliberately no un-dismiss: a dismissal self-heals
	// when discovery next sights the model, which is the only reversal the modal
	// needs.
	dismiss: async (
		providerId: string,
		modelIds: string[],
	): Promise<{ dismissed: string[]; updated: number }> => {
		return fetchJSON<{ dismissed: string[]; updated: number }>(
			`${API_BASE}/api/discovery/${encodeURIComponent(providerId)}/dismiss`,
			{
				method: "POST",
				headers: getAuthHeaders(),
				body: JSON.stringify({ model_ids: modelIds }),
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
			`${API_BASE}/api/discovery/${encodeURIComponent(providerId)}/unpin`,
			{
				method: "POST",
				headers: getAuthHeaders(),
				body: JSON.stringify({ model_ids: modelIds }),
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
};
