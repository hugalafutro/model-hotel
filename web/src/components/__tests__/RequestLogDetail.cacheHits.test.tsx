import { waitFor } from "@testing-library/react";
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
};

const onClose = () => {};

describe("RequestLogDetail cache hit coloring", () => {
	it("renders emerald color for cache hit values", async () => {
		const log: LogEntry = {
			...baseLog,
			cache_hits: {
				failover: true,
				model: true,
				provider: true,
				key: true,
				settings: true,
			},
		};
		renderWithProviders(
			<RequestLogDetail requestLog={log} onClose={onClose} />,
		);

		await waitFor(() => {
			const emeraldElements = document.querySelectorAll(".text-emerald-400");
			expect(emeraldElements.length).toBeGreaterThanOrEqual(1);
		});
	});

	it("renders amber color for cache miss values", async () => {
		const log: LogEntry = {
			...baseLog,
			cache_hits: {
				failover: false,
				model: false,
				provider: false,
				key: false,
				settings: false,
			},
		};
		renderWithProviders(
			<RequestLogDetail requestLog={log} onClose={onClose} />,
		);

		await waitFor(() => {
			const amberElements = document.querySelectorAll(".text-amber-400");
			expect(amberElements.length).toBeGreaterThanOrEqual(1);
		});
	});

	it("renders no cache hit/miss color when cache_hits is null", async () => {
		renderWithProviders(
			<RequestLogDetail requestLog={baseLog} onClose={onClose} />,
		);

		await waitFor(() => {
			expect(document.querySelectorAll(".text-emerald-400").length).toBe(0);
			expect(document.querySelectorAll(".text-amber-400").length).toBe(0);
		});
	});

	it("shows cache hit tooltip suffix for cached items", async () => {
		const log: LogEntry = {
			...baseLog,
			cache_hits: { failover: true },
		};
		renderWithProviders(
			<RequestLogDetail requestLog={log} onClose={onClose} />,
		);

		await waitFor(() => {
			const hitTitles = Array.from(document.querySelectorAll("[title]"))
				.map((el) => el.getAttribute("title") ?? "")
				.filter((t) => t.includes("cache hit"));
			expect(hitTitles.length).toBeGreaterThanOrEqual(1);
		});
	});

	it("shows cache miss tooltip suffix for uncached items", async () => {
		const log: LogEntry = {
			...baseLog,
			cache_hits: { failover: false },
		};
		renderWithProviders(
			<RequestLogDetail requestLog={log} onClose={onClose} />,
		);

		await waitFor(() => {
			const missTitles = Array.from(document.querySelectorAll("[title]"))
				.map((el) => el.getAttribute("title") ?? "")
				.filter((t) => t.includes("cache miss"));
			expect(missTitles.length).toBeGreaterThanOrEqual(1);
		});
	});
});
