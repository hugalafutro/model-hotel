import { screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { LogEntry } from "../../api/types";
import i18n from "../../i18n";
import { renderWithProviders } from "../../test/utils";
import { RequestLogDetail } from "../RequestLogDetail";

const log = {
	id: "test-id",
	provider_name: "test-provider",
	model_id: "model-1",
	status_code: 200,
	duration_ms: 500,
	proxy_overhead_ms: 0,
	cache_hits: null,
	tokens_per_second: null,
	tokens_prompt: 100,
	tokens_completion: 50,
	tokens_prompt_cache_hit: 0,
	tokens_prompt_cache_miss: 0,
	tokens_completion_reasoning: 0,
	state: "completed",
	error_message: "",
	failover_attempt: 0,
	created_at: "2024-01-01T00:00:00Z",
	endpoint_type: "chat",
} as LogEntry;

const figure = (key: string) =>
	screen.getByText(i18n.t(`components.requestLogDetail.${key}`))
		.nextElementSibling as HTMLElement;

describe("RequestLogDetail token usage", () => {
	it("keeps absent counts as dashes named for a screen reader", () => {
		renderWithProviders(
			<RequestLogDetail requestLog={log} onClose={() => {}} />,
		);
		for (const key of ["reasoning", "cacheHit", "cacheMiss"]) {
			const value = figure(key);
			expect(value).toHaveAttribute("data-absent", "true");
			expect(value).toHaveTextContent("-");
			expect(
				within(value).getByText(
					i18n.t("components.requestLogDetail.notReported"),
				),
			).toBeInTheDocument();
		}
		const prompt = figure("prompt");
		expect(within(prompt).getByText("100")).toBeInTheDocument();
		expect(prompt).not.toHaveAttribute("data-absent");
	});

	it("shows a recorded zero prompt or completion as 0, not a dash", () => {
		renderWithProviders(
			<RequestLogDetail
				requestLog={{ ...log, tokens_prompt: 0, tokens_completion: 37 }}
				onClose={() => {}}
			/>,
		);
		const prompt = figure("prompt");
		expect(prompt).toHaveTextContent(/^0$/);
		expect(prompt).not.toHaveAttribute("data-absent");
	});

	it("Regression pin: a fully cached prompt shows its cache miss as a recorded 0", () => {
		// Hit and miss are recorded as a pair when the provider reports a cache
		// read, so a 0 miss beside a non-zero hit is a real count.
		renderWithProviders(
			<RequestLogDetail
				requestLog={{ ...log, tokens_prompt_cache_hit: 80 }}
				onClose={() => {}}
			/>,
		);
		const miss = figure("cacheMiss");
		expect(miss).toHaveTextContent(/^0$/);
		expect(miss).not.toHaveAttribute("data-absent");
		expect(figure("cacheHit")).toHaveTextContent(/^80$/);
		// Reasoning is not paired, so its 0 still reads as not reported.
		expect(figure("reasoning")).toHaveAttribute("data-absent", "true");
	});
});
