import { screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { LogEntry } from "../../api/types";
import { createLogTableEntry } from "../../test/logFixtures";
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
		const entries = [createLogTableEntry({ client_ip: "203.0.113.7" })];
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
		const entries = [createLogTableEntry()];
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
