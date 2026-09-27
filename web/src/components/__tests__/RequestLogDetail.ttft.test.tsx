import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { LogEntry } from "../../api/types";
import { renderWithProviders } from "../../test/utils";
import { RequestLogDetail } from "../RequestLogDetail";

const baseLog: LogEntry = {
	id: "test-id",
	provider_id: "prov-1",
	provider_name: "test-provider",
	model_id: "model-1",
	request_hash: "hash123",
	status_code: 200,
	latency_ms: 500,
	duration_ms: 500,
	ttft_ms: 0,
	response_header_ms: 50,
	proxy_overhead_ms: 10,
	parse_ms: 1.0,
	failover_lookup_ms: 0.5,
	model_lookup_ms: 0.5,
	provider_lookup_ms: 0.5,
	key_decrypt_ms: 0.5,
	dial_ms: 10.0,
	settings_read_ms: 0.5,
	cache_hits: null,
	tokens_per_second: 12,
	tokens_prompt: 100,
	tokens_completion: 50,
	tokens_prompt_cache_hit: 0,
	tokens_prompt_cache_miss: 0,
	tokens_completion_reasoning: 0,
	streaming: false,
	state: "completed",
	virtual_key_name: "test-key",
	error_message: "",
	failover_attempt: 0,
	created_at: "2024-01-01T00:00:00Z",
	resolved_model_id: "resolved-1",
	endpoint_type: "chat",
};

const notStreamed = "Not measured for non-streaming requests";

// The TTFT tile's dash: explained on a non-streamed request (there was no
// first token to time), bare on a streamed one that never produced a token.
describe("RequestLogDetail TTFT tile", () => {
	it("explains the missing TTFT of a non-streamed request", () => {
		renderWithProviders(
			<RequestLogDetail requestLog={baseLog} onClose={() => {}} />,
		);
		expect(screen.getByTitle(notStreamed)).toHaveTextContent("-");
	});

	it("leaves a streamed request's missing TTFT unexplained", () => {
		renderWithProviders(
			<RequestLogDetail
				requestLog={{ ...baseLog, streaming: true }}
				onClose={() => {}}
			/>,
		);
		expect(screen.queryByTitle(notStreamed)).toBeNull();
	});
});
