// Shared test fixtures for Logs-related tests

import type { LogEntry } from "../api/types";

export interface MockLogEntry {
	id: string;
	created_at: string;
	request_hash: string;
	model_id: string;
	resolved_model_id: string;
	provider_name: string;
	status_code: number;
	tokens_prompt: number;
	tokens_completion: number;
	tokens_per_second: number;
	tokens_completion_reasoning: number;
	tokens_prompt_cache_hit: number;
	ttft_ms: number;
	response_header_ms: number;
	duration_ms: number;
	proxy_overhead_ms: number;
	state: "completed" | "pending" | "streaming";
	error_message: string;
	parse_ms: number;
	failover_lookup_ms: number;
	model_lookup_ms: number;
	provider_lookup_ms: number;
	key_decrypt_ms: number;
	dial_ms: number;
	virtual_key_deleted: boolean;
	virtual_key_id: string;
	virtual_key_name?: string;
	endpoint_type?: string;
	client_ip?: string;
}

export function createMockLogEntry(
	overrides: Partial<MockLogEntry> = {},
): MockLogEntry {
	const defaultEntry: MockLogEntry = {
		id: "log-001",
		created_at: "2026-05-11T10:00:00Z",
		request_hash: "abc123",
		model_id: "test-model",
		resolved_model_id: "test-model",
		provider_name: "Test",
		status_code: 200,
		tokens_prompt: 0,
		tokens_completion: 0,
		tokens_per_second: 0,
		tokens_completion_reasoning: 0,
		tokens_prompt_cache_hit: 0,
		ttft_ms: 0,
		response_header_ms: 0,
		duration_ms: 0,
		proxy_overhead_ms: 0,
		state: "completed",
		error_message: "",
		parse_ms: 0,
		failover_lookup_ms: 0,
		model_lookup_ms: 0,
		provider_lookup_ms: 0,
		key_decrypt_ms: 0,
		dial_ms: 0,
		virtual_key_deleted: false,
		virtual_key_id: "",
	};
	return { ...defaultEntry, ...overrides };
}

export function createMockLogs(
	entries: MockLogEntry[],
	total?: number,
	page: number = 1,
	perPage: number = 25,
) {
	return {
		entries,
		total: total ?? entries.length,
		page,
		per_page: perPage,
	};
}

/**
 * Build a complete LogEntry for the VirtualLogTable column tests: one
 * successful streamed request with every timing and token field populated, so
 * a test overrides only the field whose formatting it is checking.
 */
export function createLogTableEntry(
	overrides: Partial<LogEntry> = {},
): LogEntry {
	return {
		id: "log-1",
		provider_id: "prov-1",
		provider_name: "Test Provider",
		model_id: "test-provider/model-v1",
		request_hash: "abc123def456",
		status_code: 200,
		latency_ms: 150,
		duration_ms: 200,
		ttft_ms: 50,
		response_header_ms: 50,
		proxy_overhead_ms: 5,
		parse_ms: 1,
		failover_lookup_ms: 0,
		model_lookup_ms: 1,
		provider_lookup_ms: 1,
		key_decrypt_ms: 2,
		dial_ms: 10,
		settings_read_ms: 1,
		tokens_per_second: 25.5,
		tokens_prompt: 100,
		tokens_completion: 50,
		tokens_prompt_cache_hit: 0,
		tokens_prompt_cache_miss: 100,
		tokens_completion_reasoning: 0,
		streaming: true,
		state: "completed",
		virtual_key_name: "test-key",
		virtual_key_id: "vk-1",
		error_message: "",
		failover_attempt: 0,
		created_at: "2026-05-23T10:00:00Z",
		resolved_model_id: "",
		endpoint_type: "chat",
		...overrides,
	};
}
