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
	ttft_ms: 100,
	response_header_ms: 50,
	proxy_overhead_ms: 0,
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
	streaming: false,
	state: "completed",
	virtual_key_name: "test-key",
	error_message: "",
	failover_attempt: 0,
	created_at: "2024-01-01T00:00:00Z",
	resolved_model_id: "",
	endpoint_type: "chat",
};

const onClose = () => {};

describe("RequestLogDetail client IP", () => {
	it("shows the client IP where the DB row id used to be", () => {
		renderWithProviders(
			<RequestLogDetail
				requestLog={{ ...baseLog, client_ip: "203.0.113.7" }}
				onClose={onClose}
			/>,
		);
		expect(screen.getByText("Client IP")).toBeInTheDocument();
		expect(screen.getByText("203.0.113.7")).toBeInTheDocument();
		// The DB row id cell is gone entirely.
		expect(screen.queryByText("DB Row ID")).not.toBeInTheDocument();
		expect(screen.queryByText("test-id")).not.toBeInTheDocument();
	});

	it("falls back to a dash for rows without a stored IP", () => {
		// Fill every other tile so the IP cell renders the only dash.
		renderWithProviders(
			<RequestLogDetail
				requestLog={{
					...baseLog,
					tokens_prompt: 100,
					tokens_completion: 50,
					tokens_per_second: 12,
				}}
				onClose={onClose}
			/>,
		);
		expect(screen.getByText("Client IP")).toBeInTheDocument();
		expect(screen.getByText("-")).toBeInTheDocument();
	});
});
