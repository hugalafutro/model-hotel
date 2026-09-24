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
		// The kind is a labelled classification, not the raw word; the raw
		// word stays in the tooltip for anyone grepping the logs.
		expect(rows[1]).toHaveTextContent("provider saturated");
		expect(rows[1]).not.toHaveTextContent("provider_saturated");
		const saturated = within(rows[1]).getByTestId("attempt-kind");
		expect(saturated).toHaveAttribute("title", "provider_saturated");
		expect(saturated.querySelector("svg")).toHaveClass("icon-gauge");
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
		expect(row).toHaveTextContent("provider error");
		expect(row).not.toHaveTextContent("refused connection");
	});

	it("puts the breaker verdict on the indented line, under the provider", () => {
		// The verdict belongs on the row's own second line, which starts in the
		// provider column. Beside the timing it would widen the right-hand badge
		// cluster and take width from the provider and model names.
		renderWithProviders(
			<RequestLogDetail
				requestLog={{
					...baseLog,
					attempts: [
						{
							attempt: 0,
							provider_id: "prov-1",
							provider: "Z.ai Coding Plan",
							model: "glm-5.3",
							status: 200,
							duration_ms: 79837,
							hedged: true,
							breaker: "success",
						},
					],
				}}
				onClose={onClose}
			/>,
		);
		const line = screen.getByTestId("attempt-trail-meta");
		expect(line).toHaveClass("col-start-2", "flex-wrap");
		expect(line.firstElementChild).toHaveAttribute("title");
		// Every verdict names the breaker, and the served verdict reads as
		// the circuit resetting, not as money being credited.
		expect(line).toHaveTextContent("breaker: reset");
		expect(line).not.toHaveTextContent("credited");
	});

	it("says a client hangup once: the kind label, not the gateway's sentence too", () => {
		// The gateway stamps its own fixed detail on a hedged launch the client
		// abandoned (internal/proxy/hedging.go). Printed beside the kind it
		// read as the same words twice, so the label alone carries it.
		renderWithProviders(
			<RequestLogDetail
				requestLog={{
					...baseLog,
					attempts: [
						{
							attempt: 0,
							provider_id: "prov-1",
							provider: "OpenCode Go",
							model: "deepseek-flash",
							error_kind: "client_disconnect",
							detail: "client disconnected while in flight",
							duration_ms: 1703.7,
							hedged: true,
						},
					],
				}}
				onClose={onClose}
			/>,
		);
		const row = screen.getByTestId("attempt-trail-row");
		const kind = within(row).getByTestId("attempt-kind");
		expect(kind).toHaveTextContent("client disconnected");
		expect(kind).toHaveAttribute("title", "client_disconnect");
		expect(kind.querySelector("svg")).toHaveClass("icon-unplug");
		expect(row).not.toHaveTextContent("while in flight");
		expect(row).not.toHaveTextContent("client_disconnect");
	});

	it("says a deadline exit once: the kind label, not the gateway's sentence too", () => {
		renderWithProviders(
			<RequestLogDetail
				requestLog={{
					...baseLog,
					attempts: [
						{
							attempt: 1,
							provider_id: "prov-1",
							provider: "Kimi",
							model: "k2",
							error_kind: "failover_timeout",
							detail: "still in flight at the failover deadline",
							duration_ms: 60000,
							hedged: true,
						},
					],
				}}
				onClose={onClose}
			/>,
		);
		const row = screen.getByTestId("attempt-trail-row");
		const kind = within(row).getByTestId("attempt-kind");
		expect(kind).toHaveTextContent("failover timeout");
		expect(kind.querySelector("svg")).toHaveClass("icon-timer");
		expect(row).not.toHaveTextContent("failover deadline");
	});

	it("names the breaker on a strike and on a verdict it does not know", () => {
		renderWithProviders(
			<RequestLogDetail
				requestLog={{
					...baseLog,
					attempts: [
						{
							attempt: 0,
							provider_id: "prov-1",
							provider: "Kimi",
							model: "k2",
							status: 503,
							duration_ms: 12,
							breaker: "charge",
						},
						{
							attempt: 1,
							provider_id: "prov-2",
							provider: "Ollama",
							model: "k2",
							status: 200,
							duration_ms: 900,
							breaker: "wobble",
						},
					],
				}}
				onClose={onClose}
			/>,
		);
		const rows = screen.getAllByTestId("attempt-trail-row");
		expect(rows[0]).toHaveTextContent("breaker: strike");
		expect(rows[0]).not.toHaveTextContent("charged");
		// A verdict the dashboard has no word for still names the breaker.
		expect(rows[1]).toHaveTextContent("breaker: wobble");
	});

	it("keeps the gateway's words when a provider error happens to say them", () => {
		renderWithProviders(
			<RequestLogDetail
				requestLog={{
					...baseLog,
					attempts: [
						{
							attempt: 0,
							provider_id: "prov-1",
							provider: "Kimi",
							model: "k2",
							status: 200,
							error_kind: "provider_error",
							detail: "still in flight at the failover deadline",
							duration_ms: 12,
							breaker: "charge",
						},
					],
				}}
				onClose={onClose}
			/>,
		);
		expect(screen.getByTestId("attempt-trail-row")).toHaveTextContent(
			"still in flight at the failover deadline",
		);
	});

	it("renders an unknown kind raw with the plain warning glyph", () => {
		renderWithProviders(
			<RequestLogDetail
				requestLog={{
					...baseLog,
					attempts: [
						{
							attempt: 0,
							provider_id: "prov-1",
							provider: "Kimi",
							model: "k2",
							error_kind: "provider_on_fire",
							duration_ms: 12,
						},
					],
				}}
				onClose={onClose}
			/>,
		);
		const kind = screen.getByTestId("attempt-kind");
		expect(kind).toHaveTextContent("provider_on_fire");
		expect(kind.querySelector("svg")).toHaveClass("icon-alert-triangle");
	});

	it("gives a skipped attempt no verdict line: the badge already says it", () => {
		renderWithProviders(
			<RequestLogDetail
				requestLog={{
					...baseLog,
					attempts: [
						{
							attempt: -1,
							provider_id: "prov-1",
							provider: "Neuralwatt",
							model: "glm-5.3",
							duration_ms: 0,
							breaker: "skipped",
						},
					],
				}}
				onClose={onClose}
			/>,
		);
		expect(screen.queryByTestId("attempt-trail-meta")).toBeNull();
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

	it("says each thing once: no kind or detail the badges already carry", () => {
		// The three rows of a hedged 402 race, as the backend stores them.
		renderWithProviders(
			<RequestLogDetail
				requestLog={{
					...baseLog,
					attempts: [
						{
							attempt: 0,
							provider_id: "prov-0",
							provider: "Z.ai Coding Plan",
							model: "glm-5.2",
							status: 200,
							duration_ms: 13082,
							hedged: true,
							breaker: "success",
						},
						{
							attempt: 1,
							provider_id: "prov-1",
							provider: "Neuralwatt",
							model: "glm-5.2",
							status: 402,
							error_kind: "provider_error",
							detail: "HTTP 402",
							duration_ms: 575.9,
							hedged: true,
							breaker: "charge",
						},
						{
							attempt: 2,
							provider_id: "prov-2",
							provider: "Ollama Cloud",
							model: "glm-5.2",
							error_kind: "hedge_superseded",
							detail: "superseded by the winner while in flight",
							duration_ms: 672.2,
							hedged: true,
						},
					],
				}}
				onClose={onClose}
			/>,
		);
		const rows = screen.getAllByTestId("attempt-trail-row");
		// The status badge is the whole story of the 402: the kind adds nothing
		// over "the provider errored", and the detail only repeats the code.
		expect(rows[1]).not.toHaveTextContent("provider error");
		expect(rows[1]).not.toHaveTextContent("HTTP 402");
		// Same for the abandoned hedge: the SUPERSEDED badge says both.
		expect(rows[2]).toHaveTextContent("Ollama Cloud");
		expect(rows[2]).not.toHaveTextContent("hedge_superseded");
		expect(rows[2]).not.toHaveTextContent("while in flight");
	});

	it("labels 402 apart from the other client errors", () => {
		// Locale-independent: a payment-required row must not read the same as
		// any other 4xx, whichever language renders it.
		renderWithProviders(
			<RequestLogDetail
				requestLog={{
					...baseLog,
					attempts: [
						{
							attempt: 0,
							provider_id: "prov-1",
							provider: "Neuralwatt",
							model: "glm-5.2",
							status: 402,
							duration_ms: 12,
							breaker: "charge",
						},
						{
							attempt: 1,
							provider_id: "prov-2",
							provider: "Z.ai",
							model: "glm-5.2",
							status: 403,
							duration_ms: 12,
							breaker: "charge",
						},
					],
				}}
				onClose={onClose}
			/>,
		);
		const rows = screen.getAllByTestId("attempt-trail-row");
		// The badge renders "<code> <class>", so the code anchors the lookup in
		// any language and stripping it leaves the class word to compare.
		const badgeClass = (row: HTMLElement, code: number) =>
			within(row)
				.getByText(new RegExp(`^${code}\\s`))
				.textContent?.replace(String(code), "")
				.trim() ?? "";
		expect(badgeClass(rows[0], 402)).not.toBe("");
		expect(badgeClass(rows[0], 402)).not.toBe(badgeClass(rows[1], 403));
	});

	it("keeps a detail the badge does not already say", () => {
		renderWithProviders(
			<RequestLogDetail
				requestLog={{
					...baseLog,
					attempts: [
						{
							attempt: 0,
							provider_id: "prov-1",
							provider: "Neuralwatt",
							model: "glm-5.2",
							status: 429,
							error_kind: "provider_saturated",
							detail: "no capacity, retry in 30s",
							duration_ms: 41,
							breaker: "charge",
						},
					],
				}}
				onClose={onClose}
			/>,
		);
		const row = screen.getAllByTestId("attempt-trail-row")[0];
		expect(row).toHaveTextContent("no capacity, retry in 30s");
	});

	it("keeps the error kind on a 200 row that failed mid-stream", () => {
		// The upstream answered 200 and the stream broke afterwards, so the
		// status badge reads as a success and the kind is the only failure mark.
		renderWithProviders(
			<RequestLogDetail
				requestLog={{
					...baseLog,
					attempts: [
						{
							attempt: 0,
							provider_id: "prov-1",
							provider: "Ollama Cloud",
							model: "glm-5.2",
							status: 200,
							error_kind: "provider_error",
							duration_ms: 8100,
							breaker: "charge",
						},
					],
				}}
				onClose={onClose}
			/>,
		);
		expect(screen.getAllByTestId("attempt-trail-row")[0]).toHaveTextContent(
			"provider error",
		);
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
