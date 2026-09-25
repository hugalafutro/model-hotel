import { screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { LogEntry } from "../../api/types";
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

describe("RequestLogDetail token usage", () => {
	it("keeps absent counts as grey dashes instead of dropping them", () => {
		renderWithProviders(
			<RequestLogDetail requestLog={log} onClose={() => {}} />,
		);
		for (const label of ["Reasoning", "Cache Hit", "Cache Miss"]) {
			const value = screen.getByText(label).nextElementSibling as HTMLElement;
			expect(value).toHaveTextContent("-");
			expect(value.className).toContain("text-(--text-tertiary)");
		}
		const prompt = screen.getByText("Prompt").nextElementSibling as HTMLElement;
		expect(within(prompt).getByText("100")).toBeInTheDocument();
		expect(prompt.className).not.toContain("text-(--text-tertiary)");
	});

	it("shows a recorded zero prompt or completion as 0, not a dash", () => {
		renderWithProviders(
			<RequestLogDetail
				requestLog={{ ...log, tokens_prompt: 0, tokens_completion: 37 }}
				onClose={() => {}}
			/>,
		);
		const prompt = screen.getByText("Prompt").nextElementSibling as HTMLElement;
		expect(prompt).toHaveTextContent(/^0$/);
		expect(prompt.className).not.toContain("text-(--text-tertiary)");
	});
});
