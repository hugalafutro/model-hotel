import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { LogEntry } from "../../api/types";
import { renderWithProviders } from "../../test/utils";
import { RequestLogDetail } from "../RequestLogDetail";

const baseLog: LogEntry = {
	id: "test-id",
	provider_id: "prov-1",
	provider_name: "cohere",
	model_id: "Cohere/rerank-v3.5",
	request_hash: "hash123",
	status_code: 200,
	latency_ms: 500,
	duration_ms: 500,
	ttft_ms: 100,
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
	tokens_per_second: null,
	tokens_prompt: 0,
	tokens_completion: 0,
	tokens_prompt_cache_hit: 0,
	tokens_prompt_cache_miss: 0,
	tokens_completion_reasoning: 0,
	cost_usd: 0.004,
	streaming: false,
	state: "completed",
	virtual_key_name: "test-key",
	error_message: "",
	failover_attempt: 0,
	created_at: "2024-01-01T00:00:00Z",
	resolved_model_id: "",
	endpoint_type: "rerank",
};

describe("RequestLogDetail search units", () => {
	it("shows the search units a rerank row was billed for", () => {
		renderWithProviders(
			<RequestLogDetail
				requestLog={{ ...baseLog, search_units: 2 }}
				onClose={() => {}}
			/>,
		);
		expect(screen.getByText("Search Units")).toBeInTheDocument();
		expect(screen.getByText("2")).toBeInTheDocument();
	});

	it("hides the item on a row that billed no search units", () => {
		renderWithProviders(
			<RequestLogDetail requestLog={baseLog} onClose={() => {}} />,
		);
		expect(screen.queryByText("Search Units")).not.toBeInTheDocument();
	});
});
