import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { LogEntry } from "../../../api/types";
import { renderWithProviders } from "../../../test/utils";
import { RequestLogCells } from "../../logs/RequestLogCells";

const base = {
	id: "1",
	created_at: "2026-01-01T00:00:00Z",
	model_id: "openai/gpt-4",
	resolved_model_id: "",
	provider_name: "OpenAI",
	status_code: 200,
	state: "complete",
	endpoint_type: "chat",
	tokens_prompt: 10,
	tokens_completion: 20,
	tokens_prompt_cache_hit: 0,
	tokens_per_second: 30,
	response_header_ms: 12,
	ttft_ms: 34,
	duration_ms: 500,
	proxy_overhead_ms: 8,
	parse_ms: 0,
	model_lookup_ms: 0,
	provider_lookup_ms: 0,
	key_decrypt_ms: 0,
	virtual_key_name: "dev",
	virtual_key_id: "vk1",
	virtual_key_deleted: false,
	client_ip: "10.0.0.1",
	error_message: "",
} as unknown as LogEntry;

function renderCells(overrides: Partial<LogEntry> = {}) {
	return renderWithProviders(
		<table>
			<tbody>
				<tr>
					<RequestLogCells
						log={{ ...base, ...overrides }}
						nowMs={Date.parse("2026-01-01T00:00:10Z")}
						staleThresholdMs={60_000}
					/>
				</tr>
			</tbody>
		</table>,
	);
}

// The cells both request-log tables share. What is pinned here are the choices
// the two copies used to disagree on.
describe("RequestLogCells", () => {
	it("renders one cell per log column", () => {
		const { container } = renderCells();
		expect(container.querySelectorAll("td")).toHaveLength(12);
	});

	it("tints the overhead only when a phase breakdown backs it", () => {
		const { container } = renderCells({ parse_ms: 3 });
		expect(
			container.querySelectorAll("td")[9].querySelector("span")?.className,
		).toContain("text-(--accent)");

		const plain = renderCells().container.querySelectorAll("td")[9];
		expect(plain.querySelector("span")?.className).toContain("text-gray-400");
	});

	it("keeps header and TTFT timings on a cancelled request", () => {
		// Both were measured before the client went away, so hiding them would
		// throw away a real number.
		const { container } = renderCells({ error_message: "request cancelled" });
		const cells = container.querySelectorAll("td");
		expect(cells[6].textContent).toBe("12.0ms");
		expect(cells[7].textContent).toBe("34.0ms");
		// Tokens and throughput, which the cancel does invalidate, still collapse.
		expect(cells[4].textContent).toBe("Interrupted");
		expect(cells[5].textContent).toBe("-");
	});

	it("names a deleted provider and a deleted key with their own strings", () => {
		renderCells({ provider_name: "Deleted", virtual_key_deleted: true });
		expect(screen.getByTitle("Provider was deleted")).toBeInTheDocument();
		expect(screen.getAllByText("Deleted")).toHaveLength(2);
	});
});
