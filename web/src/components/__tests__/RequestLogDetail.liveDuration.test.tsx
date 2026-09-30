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

const created = Date.parse(baseLog.created_at);
const staleThresholdMs = 30 * 60 * 1000;
const live: LogEntry = { ...baseLog, state: "streaming", duration_ms: 0 };

// The duration tile of an in-progress request counts up from created_at on
// the page clock, as the row behind the modal does; a finished request keeps
// its recorded duration whatever the clock says.
describe("RequestLogDetail live duration", () => {
	it("counts an in-progress request up with the clock", () => {
		const { rerender } = renderWithProviders(
			<RequestLogDetail
				requestLog={live}
				clock={{ nowMs: created + 750, staleThresholdMs }}
				onClose={() => {}}
			/>,
		);
		expect(screen.getByText("750")).toBeInTheDocument();

		rerender(
			<RequestLogDetail
				requestLog={live}
				clock={{ nowMs: created + 850, staleThresholdMs }}
				onClose={() => {}}
			/>,
		);
		expect(screen.getByText("850")).toBeInTheDocument();
	});

	// Terminal rows that still carry duration_ms 0 must not count up: a failed
	// request, a live-shaped row past the stale threshold, and a cancelled one.
	it.each([
		[
			"failed",
			{ ...baseLog, state: "failed", duration_ms: 0 },
			staleThresholdMs,
		],
		["stale", live, 500],
		[
			"cancelled",
			{ ...live, error_kind: "client_disconnect" } as LogEntry,
			staleThresholdMs,
		],
	])("does not count up a %s request", (_name, log, threshold) => {
		renderWithProviders(
			<RequestLogDetail
				requestLog={log}
				clock={{ nowMs: created + 750, staleThresholdMs: threshold }}
				onClose={() => {}}
			/>,
		);
		expect(screen.queryByText("750")).not.toBeInTheDocument();
	});

	it("keeps a finished request's recorded duration", () => {
		renderWithProviders(
			<RequestLogDetail
				requestLog={{ ...baseLog, duration_ms: 640 }}
				clock={{ nowMs: created + 750, staleThresholdMs }}
				onClose={() => {}}
			/>,
		);
		expect(screen.getByText("640")).toBeInTheDocument();
		expect(screen.queryByText("750")).not.toBeInTheDocument();
	});
});
