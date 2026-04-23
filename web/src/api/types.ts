export interface Provider {
    id: string;
    name: string;
    base_url: string;
    masked_key: string;
    enabled: boolean;
    last_discovered_at: string | null;
    created_at: string;
    updated_at: string;
    model_count: number;
    total_tokens: number;
}

export interface CreateProviderRequest {
    name: string;
    base_url: string;
    api_key: string;
}

export interface UpdateProviderRequest {
    name?: string;
    base_url?: string;
    api_key?: string;
    enabled?: boolean;
}

export interface ProxyKey {
    id: string;
    name: string;
    created_at: string;
    key?: string;
}

export interface ModelCapabilities {
    streaming?: boolean;
    vision?: boolean;
    video_input?: boolean;
    audio_input?: boolean;
    reasoning?: boolean;
    tool_calling?: boolean;
    parallel_tool_calls?: boolean;
    structured_output?: boolean;
    pdf_upload?: boolean;
}

export interface Model {
    id: string;
    model_id: string;
    name: string;
    description: string;
    display_name: string;
    provider_id: string;
    provider_name: string;
    capabilities: string;
    params: string;
    modality: string;
    input_modalities: string;
    output_modalities: string;
    context_length: number | null;
    max_output_tokens: number | null;
    input_price_per_million: number | null;
    input_price_per_million_cache_hit: number | null;
    output_price_per_million: number | null;
    owned_by: string;
    enabled: boolean;
    created_at: string;
    last_seen_at: string;
}

export interface LogEntry {
    id: string;
    provider_id: string;
    provider_name: string;
    model_id: string;
    request_id: string;
    request_hash: string;
    status_code: number;
    latency_ms: number;
    duration_ms: number;
    ttft_ms: number;
    proxy_overhead_ms: number;
    parse_ms: number;
    model_lookup_ms: number;
    provider_lookup_ms: number;
    key_decrypt_ms: number;
    tokens_per_second: number | null;
    tokens_prompt: number;
    tokens_completion: number;
    tokens_prompt_cache_hit: number;
    tokens_prompt_cache_miss: number;
    streaming: boolean;
    virtual_key_name: string;
    virtual_key_deleted?: boolean;
    virtual_key_id?: string;
    error_message: string;
    failover_attempt: number;
    created_at: string;
}

export interface LogsResponse {
    entries: LogEntry[];
    total: number;
    page: number;
    per_page: number;
}

export interface Stats {
    total_requests_last_24h: number;
    total_requests_last_7d: number;
    by_model: Record<string, number>;
    by_provider: Record<string, number>;
    by_virtual_key: Record<string, number>;
    avg_latency_ms: number;
    error_rate: number;
    avg_overhead_ms: number;
    total_tokens_prompt: number;
    total_tokens_completion: number;
    avg_tokens_per_request: number;
}

export interface TimeSeriesPoint {
    bucket: string;
    count: number;
    tokens: number;
    errors: number;
    latency_ms: number;
    overhead_ms: number;
    provider_latency_ms: number;
}

export interface TimeSeriesStats {
    points: TimeSeriesPoint[];
}

export interface ProviderDistributionItem {
    name: string;
    count: number;
    share: number;
}

export interface ProviderDistributionStats {
    items: ProviderDistributionItem[];
}

export interface VirtualKey {
    id: string;
    name: string;
    key?: string;
    key_preview: string;
    tokens_used: number;
    last_used_at: string | null;
    created_at: string;
}

export interface SystemStats {
    app: {
        heap_alloc_mb: number;
        sys_memory_mb: number;
        goroutines: number;
        gc_cycles: number;
        memory_current_bytes: number;
        memory_limit_bytes: number;
        in_container: boolean;
    };
    db: {
        size_mb: number;
        connections: number;
        cache_hit_ratio: number;
    };
}

export interface NanoGPTUsageLimits {
    weeklyInputTokens: number | null;
    dailyInputTokens: number | null;
    dailyImages: number | null;
}

export interface NanoGPTUsageTokenInfo {
    used: number;
    remaining: number;
    percentUsed: number;
    resetAt: number;
}

export interface NanoGPTUsageDailyImages {
    used: number;
    remaining: number;
    percentUsed: number;
    resetAt: number;
}

export interface NanoGPTUsagePeriod {
    currentPeriodEnd: string;
}

export interface NanoGPTUsage {
    active: boolean;
    provider: string;
    providerStatus: string;
    providerStatusRaw: string;
    stripeSubscriptionId: string;
    cancellationReason: string | null;
    canceledAt: string | null;
    endedAt: string | null;
    cancelAt: string | null;
    cancelAtPeriodEnd: boolean;
    limits: NanoGPTUsageLimits;
    allowOverage: boolean;
    period: NanoGPTUsagePeriod;
    dailyImages: NanoGPTUsageDailyImages | null;
    dailyInputTokens: NanoGPTUsageTokenInfo | null;
    weeklyInputTokens: NanoGPTUsageTokenInfo | null;
    state: string;
    graceUntil: string | null;
}

export interface DeepSeekBalanceInfo {
    currency: "CNY" | "USD";
    total_balance: string;
    granted_balance: string;
    topped_up_balance: string;
}

export interface DeepSeekBalance {
    is_available: boolean;
    balance_infos: DeepSeekBalanceInfo[];
}

export interface FailoverEntry {
    model_uuid: string;
    model_id: string;
    provider_id: string;
    provider_name: string;
    display_name: string;
    enabled: boolean;
    context_length: number | null;
    owned_by: string;
}

export interface FailoverGroup {
    id: string;
    display_model: string;
    display_name: string | null;
    description: string;
    group_enabled: boolean;
    auto_created: boolean;
    entries: FailoverEntry[];
    total_tokens: number;
    created_at: string;
    updated_at: string;
}

export interface CreateFailoverGroupRequest {
    display_model: string;
    display_name?: string;
    description?: string;
    entry_ids: string[];
}

export interface UpdateFailoverGroupRequest {
    display_name?: string;
    description?: string;
    group_enabled?: boolean;
    priority_order?: string[];
    entry_enabled?: Record<string, boolean>;
}

export interface CandidateModel {
    model_uuid: string;
    model_id: string;
    provider_id: string;
    provider_name: string;
    display_name: string;
    context_length: number | null;
    owned_by: string;
}
