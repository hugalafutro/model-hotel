import type {
    Provider,
    CreateProviderRequest,
    UpdateProviderRequest,
    Model,
    LogsResponse,
    Stats,
    TimeSeriesStats,
    ProviderDistributionStats,
    VirtualKey,
    SystemStats,
    NanoGPTUsage,
    ZAIQuotaResponse,
    DeepSeekBalance,
    FailoverGroup,
    FailoverListResponse,
    CreateFailoverGroupRequest,
    UpdateFailoverGroupRequest,
    CandidateModel,
    SyncResult,
} from "./types";

export interface AppLogEntry {
    timestamp: string;
    level: "info" | "warning" | "error";
    source: string;
    message: string;
}

export const API_BASE = "";

let adminToken: string | null = null;

export function setAdminToken(token: string) {
    adminToken = token;
}

export function getAdminToken(): string | null {
    return adminToken;
}

function getAuthHeaders(): Record<string, string> {
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
            const response = await fetch(`${API_BASE}/api/providers`, {
                headers: getAuthHeaders(),
            });
            if (!response.ok) {
                const text = await response.text();
                throw new Error(
                    `Failed to fetch providers: ${response.status} ${text}`,
                );
            }
            return response.json();
        },
        create: async (data: CreateProviderRequest): Promise<Provider> => {
            const response = await fetch(`${API_BASE}/api/providers`, {
                method: "POST",
                headers: getAuthHeaders(),
                body: JSON.stringify(data),
            });
            if (!response.ok) {
                const text = await response.text();
                throw new Error(
                    `Failed to create provider: ${response.status} ${text}`,
                );
            }
            return response.json();
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
            const response = await fetch(`${API_BASE}/api/providers/${id}`, {
                method: "PUT",
                headers: getAuthHeaders(),
                body: JSON.stringify(data),
            });
            if (!response.ok) {
                const text = await response.text();
                throw new Error(
                    `Failed to update provider: ${response.status} ${text}`,
                );
            }
            return response.json();
        },
        discover: async (id: string): Promise<{ discovered: number }> => {
            const response = await fetch(
                `${API_BASE}/api/providers/${id}/discover`,
                {
                    method: "POST",
                    headers: getAuthHeaders(),
                },
            );
            if (!response.ok) {
                const text = await response.text();
                throw new Error(
                    `Failed to discover models: ${response.status} ${text}`,
                );
            }
            return response.json();
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
            const response = await fetch(
                `${API_BASE}/api/providers/discover-all`,
                {
                    method: "POST",
                    headers: getAuthHeaders(),
                },
            );
            if (!response.ok) {
                const text = await response.text();
                throw new Error(
                    `Failed to discover all: ${response.status} ${text}`,
                );
            }
            return response.json();
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
            const response = await fetch(
                `${API_BASE}/api/providers/refresh-quotas`,
                {
                    method: "POST",
                    headers: getAuthHeaders(),
                },
            );
            if (!response.ok) {
                const text = await response.text();
                throw new Error(
                    `Failed to refresh quotas: ${response.status} ${text}`,
                );
            }
            return response.json();
        },
        getUsage: async (
            id: string,
        ): Promise<NanoGPTUsage | ZAIQuotaResponse> => {
            const response = await fetch(
                `${API_BASE}/api/providers/${id}/usage`,
                {
                    headers: getAuthHeaders(),
                },
            );
            if (!response.ok) {
                const text = await response.text();
                throw new Error(
                    `Failed to fetch usage: ${response.status} ${text}`,
                );
            }
            return response.json();
        },
        getBalance: async (id: string): Promise<DeepSeekBalance> => {
            const response = await fetch(
                `${API_BASE}/api/providers/${id}/balance`,
                {
                    headers: getAuthHeaders(),
                },
            );
            if (!response.ok) {
                const text = await response.text();
                throw new Error(
                    `Failed to fetch balance: ${response.status} ${text}`,
                );
            }
            return response.json();
        },
    },

    models: {
        list: async (providerId?: string): Promise<Model[]> => {
            const url = providerId
                ? `${API_BASE}/api/models?provider_id=${providerId}`
                : `${API_BASE}/api/models`;
            const response = await fetch(url, {
                headers: getAuthHeaders(),
            });
            if (!response.ok) {
                const text = await response.text();
                throw new Error(
                    `Failed to fetch models: ${response.status} ${text}`,
                );
            }
            return response.json();
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
            const response = await fetch(`${API_BASE}/api/models/${id}`, {
                method: "PATCH",
                headers: getAuthHeaders(),
                body: JSON.stringify(data),
            });
            if (!response.ok) {
                const text = await response.text();
                throw new Error(
                    `Failed to update model: ${response.status} ${text}`,
                );
            }
            return response.json();
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
            const response = await fetch(`${API_BASE}/api/models/${id}/test`, {
                method: "POST",
                headers: getAuthHeaders(),
            });
            if (!response.ok) {
                const text = await response.text();
                throw new Error(`Test failed: ${response.status} ${text}`);
            }
            return response.json();
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
            const searchParams = new URLSearchParams();
            if (params.page)
                searchParams.append("page", params.page.toString());
            if (params.per_page)
                searchParams.append("per_page", params.per_page.toString());
            if (params.model_id)
                searchParams.append("model_id", params.model_id);
            if (params.provider_id)
                searchParams.append("provider_id", params.provider_id);
            if (params.status_code)
                searchParams.append("status_code", params.status_code);
            if (params.from) searchParams.append("from", params.from);
            if (params.to) searchParams.append("to", params.to);
            if (params.sort_by) searchParams.append("sort_by", params.sort_by);
            if (params.sort_dir)
                searchParams.append("sort_dir", params.sort_dir);

            const response = await fetch(
                `${API_BASE}/api/logs?${searchParams}`,
                {
                    headers: getAuthHeaders(),
                },
            );
            if (!response.ok) {
                const text = await response.text();
                throw new Error(
                    `Failed to fetch logs: ${response.status} ${text}`,
                );
            }
            return response.json();
        },
        purge: async (olderThan: string): Promise<void> => {
            const response = await fetch(`${API_BASE}/api/logs/purge`, {
                method: "DELETE",
                headers: getAuthHeaders(),
                body: JSON.stringify({ older_than: olderThan }),
            });
            if (!response.ok && response.status !== 204) {
                const text = await response.text();
                throw new Error(
                    `Failed to purge logs: ${response.status} ${text}`,
                );
            }
        },
    },

    appLogs: {
        list: async (params?: {
            limit?: number;
            after?: string;
        }): Promise<AppLogEntry[]> => {
            const searchParams = new URLSearchParams();
            if (params?.limit) {
                searchParams.append("limit", params.limit.toString());
            }
            if (params?.after) {
                searchParams.append("after", params.after);
            }

            const qs = searchParams.toString();
            const url = `${API_BASE}/api/logs/app${qs ? `?${qs}` : ""}`;

            const response = await fetch(url, {
                headers: getAuthHeaders(),
            });
            if (!response.ok) {
                const text = await response.text();
                throw new Error(
                    `Failed to fetch app logs: ${response.status} ${text}`,
                );
            }
            return response.json();
        },
        purge: async (): Promise<{ deleted: number }> => {
            const response = await fetch(`${API_BASE}/api/logs/app`, {
                method: "DELETE",
                headers: getAuthHeaders(),
            });
            if (!response.ok) {
                const text = await response.text();
                throw new Error(
                    `Failed to purge app logs: ${response.status} ${text}`,
                );
            }
            return response.json();
        },
        history: async (params?: {
            level?: string;
            source?: string;
            search?: string;
            from?: string;
            to?: string;
            page?: number;
            per_page?: number;
        }): Promise<{
            entries: AppLogEntry[];
            total: number;
            page: number;
            per_page: number;
        }> => {
            const searchParams = new URLSearchParams();
            searchParams.set("history", "true");
            if (params?.level) searchParams.set("level", params.level);
            if (params?.source) searchParams.set("source", params.source);
            if (params?.search) searchParams.set("search", params.search);
            if (params?.from) searchParams.set("from", params.from);
            if (params?.to) searchParams.set("to", params.to);
            if (params?.page) searchParams.set("page", params.page.toString());
            if (params?.per_page)
                searchParams.set("per_page", params.per_page.toString());

            const response = await fetch(
                `${API_BASE}/api/logs/app?${searchParams.toString()}`,
                {
                    headers: getAuthHeaders(),
                },
            );
            if (!response.ok) {
                const text = await response.text();
                throw new Error(
                    `Failed to fetch app log history: ${response.status} ${text}`,
                );
            }
            return response.json();
        },
    },

    stats: {
        get: async (opts?: {
            period?: string;
            excludeDeleted?: boolean;
            metric?: "requests" | "tokens";
        }): Promise<Stats> => {
            const params = new URLSearchParams();
            if (opts?.period) params.set("period", opts.period);
            if (opts?.excludeDeleted) params.set("exclude_deleted", "true");
            if (opts?.metric) params.set("metric", opts.metric);
            const qs = params.toString();
            const url = qs
                ? `${API_BASE}/api/stats?${qs}`
                : `${API_BASE}/api/stats`;
            const response = await fetch(url, {
                headers: getAuthHeaders(),
            });
            if (!response.ok) {
                const text = await response.text();
                throw new Error(
                    `Failed to fetch stats: ${response.status} ${text}`,
                );
            }
            return response.json();
        },
        getTimeSeries: async (opts?: {
            period?: string;
            excludeDeleted?: boolean;
        }): Promise<TimeSeriesStats> => {
            const params = new URLSearchParams();
            if (opts?.period) params.set("period", opts.period);
            if (opts?.excludeDeleted) params.set("exclude_deleted", "true");
            const qs = params.toString();
            const url = qs
                ? `${API_BASE}/api/stats/timeseries?${qs}`
                : `${API_BASE}/api/stats/timeseries`;
            const response = await fetch(url, {
                headers: getAuthHeaders(),
            });
            if (!response.ok) {
                const text = await response.text();
                throw new Error(
                    `Failed to fetch time-series stats: ${response.status} ${text}`,
                );
            }
            return response.json();
        },
        getProviderDistribution: async (opts?: {
            period?: string;
            metric?: string;
            excludeDeleted?: boolean;
        }): Promise<ProviderDistributionStats> => {
            const params = new URLSearchParams();
            if (opts?.period) params.set("period", opts.period);
            if (opts?.metric) params.set("metric", opts.metric);
            if (opts?.excludeDeleted) params.set("exclude_deleted", "true");
            const qs = params.toString();
            const url = qs
                ? `${API_BASE}/api/stats/provider-distribution?${qs}`
                : `${API_BASE}/api/stats/provider-distribution`;
            const response = await fetch(url, {
                headers: getAuthHeaders(),
            });
            if (!response.ok) {
                const text = await response.text();
                throw new Error(
                    `Failed to fetch provider distribution: ${response.status} ${text}`,
                );
            }
            return response.json();
        },
    },

    settings: {
        get: async (): Promise<Record<string, string>> => {
            const response = await fetch(`${API_BASE}/api/settings`, {
                headers: getAuthHeaders(),
            });
            if (!response.ok) {
                const text = await response.text();
                throw new Error(
                    `Failed to fetch settings: ${response.status} ${text}`,
                );
            }
            return response.json();
        },
        update: async (
            settings: Record<string, string>,
        ): Promise<Record<string, string>> => {
            const response = await fetch(`${API_BASE}/api/settings`, {
                method: "PUT",
                headers: getAuthHeaders(),
                body: JSON.stringify(settings),
            });
            if (!response.ok) {
                const text = await response.text();
                throw new Error(
                    `Failed to update settings: ${response.status} ${text}`,
                );
            }
            return response.json();
        },
    },

    virtualKeys: {
        list: async (): Promise<VirtualKey[]> => {
            const response = await fetch(`${API_BASE}/api/virtual-keys`, {
                headers: getAuthHeaders(),
            });
            if (!response.ok) {
                const text = await response.text();
                throw new Error(
                    `Failed to fetch virtual keys: ${response.status} ${text}`,
                );
            }
            return response.json();
        },
        create: async (name: string): Promise<VirtualKey> => {
            const response = await fetch(`${API_BASE}/api/virtual-keys`, {
                method: "POST",
                headers: getAuthHeaders(),
                body: JSON.stringify({ name }),
            });
            if (!response.ok) {
                const text = await response.text();
                throw new Error(
                    `Failed to create virtual key: ${response.status} ${text}`,
                );
            }
            return response.json();
        },
        get: async (id: string): Promise<VirtualKey> => {
            const response = await fetch(`${API_BASE}/api/virtual-keys/${id}`, {
                headers: getAuthHeaders(),
            });
            if (!response.ok) {
                const text = await response.text();
                throw new Error(
                    `Failed to fetch virtual key: ${response.status} ${text}`,
                );
            }
            return response.json();
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
            const response = await fetch(`${API_BASE}/api/system`, {
                headers: getAuthHeaders(),
            });
            if (!response.ok) {
                const text = await response.text();
                throw new Error(
                    `Failed to fetch system stats: ${response.status} ${text}`,
                );
            }
            return response.json();
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
            const response = await fetch(`${API_BASE}/api/chat/completions`, {
                method: "POST",
                headers: getAuthHeaders(),
                body: JSON.stringify(body),
            });
            if (!response.ok) {
                const text = await response.text();
                throw new Error(`Chat failed: ${response.status} ${text}`);
            }
            return response;
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
            const response = await fetch(`${API_BASE}/api/chat/chat`, {
                method: "POST",
                headers: getAuthHeaders(),
                body: JSON.stringify(body),
                ...(body.signal ? { signal: body.signal } : {}),
            });
            if (!response.ok) {
                const text = await response.text();
                throw new Error(`Chat failed: ${response.status} ${text}`);
            }
            return response;
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
            const response = await fetch(`${API_BASE}/api/chat/arena`, {
                method: "POST",
                headers: getAuthHeaders(),
                body: JSON.stringify(body),
                ...(body.signal ? { signal: body.signal } : {}),
            });
            if (!response.ok) {
                const text = await response.text();
                throw new Error(`Arena failed: ${response.status} ${text}`);
            }
            return response;
        },
    },

    failoverGroups: {
        list: async (): Promise<FailoverListResponse> => {
            const response = await fetch(`${API_BASE}/api/failover-groups`, {
                headers: getAuthHeaders(),
            });
            if (!response.ok) {
                const text = await response.text();
                throw new Error(
                    `Failed to fetch failover groups: ${response.status} ${text}`,
                );
            }
            return response.json();
        },
        get: async (id: string): Promise<FailoverGroup> => {
            const response = await fetch(
                `${API_BASE}/api/failover-groups/${id}`,
                {
                    headers: getAuthHeaders(),
                },
            );
            if (!response.ok) {
                const text = await response.text();
                throw new Error(
                    `Failed to fetch failover group: ${response.status} ${text}`,
                );
            }
            return response.json();
        },
        create: async (
            data: CreateFailoverGroupRequest,
        ): Promise<FailoverGroup> => {
            const response = await fetch(`${API_BASE}/api/failover-groups`, {
                method: "POST",
                headers: getAuthHeaders(),
                body: JSON.stringify(data),
            });
            if (!response.ok) {
                const text = await response.text();
                throw new Error(
                    `Failed to create failover group: ${response.status} ${text}`,
                );
            }
            return response.json();
        },
        update: async (
            id: string,
            data: UpdateFailoverGroupRequest,
        ): Promise<FailoverGroup> => {
            const response = await fetch(
                `${API_BASE}/api/failover-groups/${id}`,
                {
                    method: "PUT",
                    headers: getAuthHeaders(),
                    body: JSON.stringify(data),
                },
            );
            if (!response.ok) {
                const text = await response.text();
                throw new Error(
                    `Failed to update failover group: ${response.status} ${text}`,
                );
            }
            return response.json();
        },
        delete: async (id: string): Promise<void> => {
            const response = await fetch(
                `${API_BASE}/api/failover-groups/${id}`,
                {
                    method: "DELETE",
                    headers: getAuthHeaders(),
                },
            );
            if (!response.ok && response.status !== 204) {
                throw new Error("Failed to delete failover group");
            }
        },
        sync: async (): Promise<SyncResult> => {
            const response = await fetch(
                `${API_BASE}/api/failover-groups/sync`,
                {
                    method: "POST",
                    headers: getAuthHeaders(),
                },
            );
            if (!response.ok) {
                const text = await response.text();
                throw new Error(
                    `Failed to sync failover groups: ${response.status} ${text}`,
                );
            }
            return response.json();
        },
        candidates: async (): Promise<CandidateModel[]> => {
            const response = await fetch(
                `${API_BASE}/api/failover-groups/candidates`,
                {
                    headers: getAuthHeaders(),
                },
            );
            if (!response.ok) {
                const text = await response.text();
                throw new Error(
                    `Failed to fetch candidates: ${response.status} ${text}`,
                );
            }
            return response.json();
        },
    },
};
