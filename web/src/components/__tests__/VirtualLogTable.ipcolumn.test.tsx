import { screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { LogEntry } from "../../api/types";
import { renderWithProviders } from "../../test/utils";
import { VirtualLogTable } from "../VirtualLogTable";

const mockGetVirtualItems = vi.fn();
const mockGetTotalSize = vi.fn();
const mockMeasureElement = vi.fn();

vi.mock("@tanstack/react-virtual", () => ({
	useVirtualizer: vi.fn(() => ({
		getVirtualItems: mockGetVirtualItems,
		getTotalSize: mockGetTotalSize,
		measureElement: mockMeasureElement,
	})),
}));

function createLogEntry(overrides: Partial<LogEntry> = {}): LogEntry {
	return {
		id: "log-1",
		provider_id: "prov-1",
		provider_name: "Test Provider",
		model_id: "test-provider/model-v1",
		request_hash: "abc123def456",
		status_code: 200,
		latency_ms: 150,
		duration_ms: 200,
		ttft_ms: 50,
		response_header_ms: 50,
		proxy_overhead_ms: 5,
		parse_ms: 1,
		failover_lookup_ms: 0,
		model_lookup_ms: 1,
		provider_lookup_ms: 1,
		key_decrypt_ms: 2,
		dial_ms: 10,
		settings_read_ms: 1,
		tokens_per_second: 25.5,
		tokens_prompt: 100,
		tokens_completion: 50,
		tokens_prompt_cache_hit: 0,
		tokens_prompt_cache_miss: 100,
		tokens_completion_reasoning: 0,
		streaming: true,
		state: "completed",
		virtual_key_name: "test-key",
		virtual_key_id: "vk-1",
		error_message: "",
		failover_attempt: 0,
		created_at: "2026-05-23T10:00:00Z",
		resolved_model_id: "",
		endpoint_type: "chat",
		...overrides,
	};
}

const defaultProps = {
	entries: [] as LogEntry[],
	total: 0,
	hasBefore: false,
	hasAfter: false,
	isLoadingBefore: false,
	isLoadingAfter: false,
	onFetchNewer: vi.fn(),
	onFetchOlder: vi.fn(),
	onRowClick: vi.fn(),
	nowMs: Date.now(),
	staleThresholdMs: 30 * 60 * 1000,
	sortDir: "desc" as const,
	onSortToggle: vi.fn(),
};

describe("VirtualLogTable IP column", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		mockGetVirtualItems.mockReturnValue([]);
		mockGetTotalSize.mockReturnValue(0);
		mockMeasureElement.mockImplementation(() => {});
	});

	it("renders the IP header and the entry's client IP", () => {
		const entries = [createLogEntry({ client_ip: "203.0.113.7" })];
		mockGetVirtualItems.mockReturnValue([
			{ index: 0, key: entries[0].id, start: 0, end: 29 },
		]);
		mockGetTotalSize.mockReturnValue(29);

		renderWithProviders(
			<VirtualLogTable {...defaultProps} entries={entries} total={1} />,
		);

		expect(screen.getByText("IP")).toBeInTheDocument();
		expect(screen.getByText("203.0.113.7")).toBeInTheDocument();
	});

	it("renders a dash for entries without a stored IP", () => {
		// Every other cell of this entry has a non-dash value, so the only "-"
		// in the row is the IP cell's fallback.
		const entries = [createLogEntry()];
		mockGetVirtualItems.mockReturnValue([
			{ index: 0, key: entries[0].id, start: 0, end: 29 },
		]);
		mockGetTotalSize.mockReturnValue(29);

		renderWithProviders(
			<VirtualLogTable {...defaultProps} entries={entries} total={1} />,
		);

		expect(screen.getByText("-")).toBeInTheDocument();
	});
});
