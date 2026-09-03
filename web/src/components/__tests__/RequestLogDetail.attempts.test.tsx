import { screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { LogEntry } from "../../api/types";
import { renderWithProviders } from "../../test/utils";
import { RequestLogDetail } from "../RequestLogDetail";

const baseLog: LogEntry = {
	id: "test-id",
	provider_id: "prov-2",
	provider_name: "Ollama",
	model_id: "hotel/glm53",
	request_hash: "hash123",
	status_code: 200,
	latency_ms: 8700,
	duration_ms: 8700,
	ttft_ms: 1561,
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
	failover_attempt: 1,
	created_at: "2026-08-31T14:48:37Z",
	resolved_model_id: "glm-5.3",
	endpoint_type: "chat",
};

const onClose = () => {};

// Locale-independent: testids, provider names, statuses and raw detail text.
describe("RequestLogDetail attempt trail", () => {
	it("renders one row per attempt in order, skips and hedges marked", () => {
		renderWithProviders(
			<RequestLogDetail
				requestLog={{
					...baseLog,
					attempts: [
						{
							attempt: -1,
							provider_id: "prov-0",
							provider: "Z.ai",
							model: "glm-5.3",
							duration_ms: 0,
							detail: "circuit breaker open",
							breaker: "skipped",
						},
						{
							attempt: 0,
							provider_id: "prov-1",
							provider: "Neuralwatt",
							model: "glm-5.3",
							status: 429,
							error_kind: "provider_saturated",
							detail: "concurrent_budget_exceeded",
							phrase: "concurrent_budget_exceeded",
							duration_ms: 412,
							hedged: true,
							breaker: "noop",
						},
						{
							attempt: 1,
							provider_id: "prov-2",
							provider: "Ollama",
							model: "glm-5.3",
							status: 200,
							duration_ms: 8299,
							ttft_ms: 1561,
							breaker: "success",
						},
						{
							// A hedged launch abandoned when Ollama won: no status,
							// and not a failure.
							attempt: 2,
							provider_id: "prov-3",
							provider: "Kimi",
							model: "glm-5.3",
							error_kind: "hedge_superseded",
							detail: "superseded by the winner while in flight",
							duration_ms: 1602,
							hedged: true,
						},
					],
				}}
				onClose={onClose}
			/>,
		);
		const trail = screen.getByTestId("attempt-trail");
		const rows = within(trail).getAllByTestId("attempt-trail-row");
		expect(rows).toHaveLength(4);
		expect(rows[3]).toHaveTextContent("Kimi");
		expect(
			within(rows[3]).getByTestId("attempt-superseded"),
		).toBeInTheDocument();
		// Not painted as a failure: no red "no response" badge on that row.
		expect(rows[3].querySelector(".ui-badge-red")).toBeNull();
		expect(rows[0]).toHaveTextContent("Z.ai");
		expect(rows[0]).toHaveTextContent("circuit breaker open");
		// A skipped candidate has no attempt number and no status.
		expect(rows[0]).not.toHaveTextContent("429");
		expect(rows[1]).toHaveTextContent("Neuralwatt");
		expect(rows[1]).toHaveTextContent("429");
		expect(rows[1]).toHaveTextContent("provider_saturated");
		expect(rows[1]).toHaveTextContent("concurrent_budget_exceeded");
		expect(rows[2]).toHaveTextContent("Ollama");
		expect(rows[2]).toHaveTextContent("200");
	});

	it("leaves out a detail that is the row's own error message", () => {
		// Stored before the backend stopped writing the terminal attempt's
		// detail: the same JSON, whitespace collapsed, as error_message.
		const error =
			'{"error": {"code": "1234",\n  "message": "Internal network failure, please try again later."}}';
		renderWithProviders(
			<RequestLogDetail
				requestLog={{
					...baseLog,
					status_code: 500,
					error_message: error,
					attempts: [
						{
							attempt: 0,
							provider_id: "prov-1",
							provider: "Neuralwatt",
							model: "glm-5.3",
							status: 503,
							error_kind: "provider_error",
							detail: "upstream is down for maintenance",
							duration_ms: 90,
							breaker: "charge",
						},
						{
							attempt: 1,
							provider_id: "prov-2",
							provider: "Z.ai",
							model: "glm-5.3-flash",
							status: 500,
							error_kind: "provider_error",
							detail:
								'{"error": {"code": "1234", "message": "Internal network failure, please try again later."}}',
							duration_ms: 14917,
							breaker: "charge",
						},
					],
				}}
				onClose={onClose}
			/>,
		);
		const rows = screen.getAllByTestId("attempt-trail-row");
		expect(rows[0]).toHaveTextContent("down for maintenance");
		expect(rows[1]).toHaveTextContent("Z.ai");
		expect(rows[1]).toHaveTextContent("500");
		expect(rows[1]).not.toHaveTextContent("Internal network failure");
	});

	it("leaves out a last detail the terminal message quotes", () => {
		// A transport failure: the attempt closes with the raw error and the
		// terminal message wraps that same text.
		const detail =
			'Post "http://172.20.0.1:21434/v1/chat/completions": proxy: refused connection to private/reserved IP 172.20.0.1';
		renderWithProviders(
			<RequestLogDetail
				requestLog={{
					...baseLog,
					status_code: 502,
					error_message: `provider "Ollama" failed on attempt 1: ${detail}`,
					attempts: [
						{
							attempt: 0,
							provider_id: "prov-1",
							provider: "Ollama",
							model: "smollm2:135m",
							error_kind: "provider_error",
							detail,
							duration_ms: 384,
							breaker: "charge",
						},
					],
				}}
				onClose={onClose}
			/>,
		);
		const row = screen.getByTestId("attempt-trail-row");
		expect(row).toHaveTextContent("provider_error");
		expect(row).not.toHaveTextContent("refused connection");
	});

	it("leaves out a last detail the terminal message quotes mid-sentence", () => {
		// A hedged loser with no provider sentence carries the bare status,
		// which the terminal message embeds rather than ends with.
		renderWithProviders(
			<RequestLogDetail
				requestLog={{
					...baseLog,
					status_code: 502,
					error_message: 'provider "b" returned HTTP 503 on attempt 1',
					attempts: [
						{
							attempt: 0,
							provider_id: "prov-1",
							provider: "b",
							model: "glm-5.3",
							status: 503,
							error_kind: "provider_error",
							detail: "HTTP 503",
							duration_ms: 12,
							hedged: true,
							breaker: "charge",
						},
					],
				}}
				onClose={onClose}
			/>,
		);
		const row = screen.getByTestId("attempt-trail-row");
		expect(row).toHaveTextContent("503");
		expect(row).not.toHaveTextContent("HTTP 503");
	});

	it("renders nothing for a row without a trail", () => {
		renderWithProviders(
			<RequestLogDetail requestLog={baseLog} onClose={onClose} />,
		);
		expect(screen.queryByTestId("attempt-trail")).not.toBeInTheDocument();
		renderWithProviders(
			<RequestLogDetail
				requestLog={{ ...baseLog, attempts: [] }}
				onClose={onClose}
			/>,
		);
		expect(screen.queryByTestId("attempt-trail")).not.toBeInTheDocument();
	});
});
