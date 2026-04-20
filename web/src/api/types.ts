export interface Setting {
  key: string
  value: string
}

export interface Provider {
  id: string;
  name: string;
  base_url: string;
  masked_key: string;
  enabled: boolean;
  last_discovered_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface CreateProviderRequest {
  name: string;
  base_url: string;
  api_key: string;
}

export interface ProxyKey {
  id: string;
  name: string;
  created_at: string;
  key?: string;
}

export interface Model {
  id: string;
  model_id: string;
  display_name: string;
  provider_id: string;
  provider_name: string;
  capabilities: string;
  owned_by: string;
  enabled: boolean;
  created_at: string;
  last_seen_at: string;
}

export interface LogEntry {
  id: string;
  provider_id: string;
  model_id: string;
  request_id: string;
  status_code: number;
  latency_ms: number;
  tokens_prompt: number;
  tokens_completion: number;
  streaming: boolean;
  error_message: string;
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
  avg_latency_ms: number;
  error_rate: number;
  total_tokens_prompt: number;
  total_tokens_completion: number;
}