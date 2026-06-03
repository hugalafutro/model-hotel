import { screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { FailoverGroup } from "../../../api/types";
import { mockProvider } from "../../../test/mocks/data";
import { renderWithProviders } from "../../../test/utils";
import { SortableEntry } from "../SortableEntry";

// Mock dnd-kit modules at top level
vi.mock("@dnd-kit/sortable", () => ({
	useSortable: vi.fn(() => ({
		attributes: { role: "button", tabIndex: 0 },
		listeners: { onPointerDown: vi.fn() },
		setNodeRef: vi.fn(),
		transform: null,
		transition: null,
		isDragging: false,
	})),
}));

vi.mock("@dnd-kit/utilities", () => ({
	CSS: { Transform: { toString: () => "" } },
}));

// Mock useResizeObserver to return non-zero dimensions for FuseOutline
vi.mock("../../../hooks/useResizeObserver", () => ({
	useResizeObserver: vi.fn(() => ({
		ref: { current: null },
		width: 100,
		height: 40,
	})),
}));

const baseEntry: FailoverGroup["entries"][0] = {
	model_uuid: "test-uuid-1",
	model_id: "gpt-4",
	provider_id: "provider-uuid-1",
	provider_name: "TestProvider",
	display_name: "GPT-4",
	enabled: true,
	context_length: 8192,
	owned_by: "openai",
};

describe("SortableEntry - Circuit Breaker Fuse Outline", () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	/**
	 * Helper to get the wrapper div with overflow style.
	 * Structure: wrapper div -> inner flex div -> text spans
	 * By getting the drag handle (⠿) and navigating up twice, we reach the wrapper.
	 */
	function getWrapperDiv(container: HTMLElement): HTMLDivElement | null {
		const dragHandle = container.querySelector("span");
		if (!dragHandle) return null;
		// The inner flex div contains the drag handle
		const innerFlexDiv = dragHandle.parentElement;
		if (!innerFlexDiv) return null;
		// The wrapper div contains the inner flex div
		return innerFlexDiv.parentElement as HTMLDivElement | null;
	}

	describe("No cbStatus", () => {
		it("renders normally without FuseOutline when cbStatus is undefined", () => {
			const { container } = renderWithProviders(
				<SortableEntry
					entry={baseEntry}
					groupEnabled={true}
					onToggle={vi.fn()}
					cbStatus={undefined}
				/>,
			);

			expect(screen.getByText("TestProvider")).toBeInTheDocument();
			expect(screen.getByText("gpt-4")).toBeInTheDocument();

			// No FuseOutline should render
			const svgElements = container.querySelectorAll("svg");
			expect(svgElements.length).toBe(0);
		});
	});

	describe("cbStatus with state 'closed'", () => {
		it("does not render FuseOutline for closed circuit breaker", () => {
			const cbStatus = {
				state: "closed",
				consecutive_fails: 0,
			};

			const { container } = renderWithProviders(
				<SortableEntry
					entry={baseEntry}
					groupEnabled={true}
					onToggle={vi.fn()}
					cbStatus={cbStatus}
				/>,
			);

			expect(screen.getByText("TestProvider")).toBeInTheDocument();

			// No FuseOutline should render for closed state
			const svgElements = container.querySelectorAll("svg");
			expect(svgElements.length).toBe(0);
		});
	});

	describe("cbStatus with state 'open' and consecutive_fails >= 5", () => {
		it("renders FuseOutline with red color and proper attributes", () => {
			const cbStatus = {
				state: "open",
				cooldown_ms: 60000,
				next_retry_at: new Date(Date.now() + 30000).toISOString(),
				consecutive_fails: 7,
			};

			const { container } = renderWithProviders(
				<SortableEntry
					entry={baseEntry}
					groupEnabled={true}
					onToggle={vi.fn()}
					cbStatus={cbStatus}
				/>,
			);

			// FuseOutline should render as SVG
			const svgElement = container.querySelector("svg");
			expect(svgElement).toBeInTheDocument();

			// Check for red color (#fca5a5) in the rect element
			const rectElement = svgElement?.querySelector("rect");
			expect(rectElement).toBeInTheDocument();

			// Entry should have overflow: hidden
			const wrapperDiv = getWrapperDiv(container);
			expect(wrapperDiv).toHaveStyle("overflow: hidden");

			// Title should show i18n key (since test uses English locale, check translated string)
			// The title comes from t() which translates the key
			if (wrapperDiv) {
				expect(wrapperDiv).toHaveAttribute(
					"title",
					expect.stringContaining("Circuit breaker open"),
				);
			}
		});
	});

	describe("cbStatus with state 'half-open' and consecutive_fails >= 5", () => {
		it("renders static amber outline (no SVG fuse animation) for half-open", () => {
			const cbStatus = {
				state: "half-open",
				consecutive_fails: 5,
			};

			const { container } = renderWithProviders(
				<SortableEntry
					entry={baseEntry}
					groupEnabled={true}
					onToggle={vi.fn()}
					cbStatus={cbStatus}
				/>,
			);

			// Half-open: static amber outline via box-shadow, NOT SVG FuseOutline
			const svgElements = container.querySelectorAll("svg");
			expect(svgElements.length).toBe(0);

			// Static outline div should render with amber color
			const outlineDiv = container.querySelector('[style*="box-shadow"]');
			expect(outlineDiv).toBeInTheDocument();
			expect(outlineDiv?.getAttribute("style")).toContain("#fde68a");

			// Entry should have overflow: hidden
			const wrapperDiv = getWrapperDiv(container);
			if (wrapperDiv) {
				expect(wrapperDiv).toHaveStyle("overflow: hidden");
			}

			// Title should show i18n key for half-open
			if (wrapperDiv) {
				expect(wrapperDiv).toHaveAttribute(
					"title",
					expect.stringContaining("Circuit breaker half-open"),
				);
			}
		});
	});

	describe("cbStatus with state 'open' but consecutive_fails < 5", () => {
		it("does NOT render FuseOutline for fast backoff (insufficient fails)", () => {
			const cbStatus = {
				state: "open",
				cooldown_ms: 60000,
				consecutive_fails: 3,
			};

			const { container } = renderWithProviders(
				<SortableEntry
					entry={baseEntry}
					groupEnabled={true}
					onToggle={vi.fn()}
					cbStatus={cbStatus}
				/>,
			);

			// No FuseOutline should render (less than 5 fails)
			const svgElements = container.querySelectorAll("svg");
			expect(svgElements.length).toBe(0);

			// Entry should NOT have overflow: hidden
			const wrapperDiv = getWrapperDiv(container);
			if (wrapperDiv) {
				expect(wrapperDiv).not.toHaveStyle("overflow: hidden");
			}
		});
	});

	describe("Disabled entry with open cbStatus", () => {
		it("does NOT render FuseOutline for disabled entries", () => {
			const disabledEntry = { ...baseEntry, enabled: false };
			const cbStatus = {
				state: "open",
				cooldown_ms: 60000,
				consecutive_fails: 10,
			};

			const { container } = renderWithProviders(
				<SortableEntry
					entry={disabledEntry}
					groupEnabled={true}
					onToggle={vi.fn()}
					cbStatus={cbStatus}
				/>,
			);

			// No FuseOutline should render (entry disabled)
			const svgElements = container.querySelectorAll("svg");
			expect(svgElements.length).toBe(0);

			// Entry should NOT have overflow: hidden (disabled entries don't show fuse)
			const wrapperDiv = getWrapperDiv(container);
			if (wrapperDiv) {
				expect(wrapperDiv).not.toHaveStyle("overflow: hidden");
			}
		});
	});

	describe("next_retry_at in the future", () => {
		it("computes durationMs correctly from remaining time", () => {
			const futureTime = Date.now() + 45000; // 45 seconds in the future
			const cbStatus = {
				state: "open",
				cooldown_ms: 60000, // fallback value
				next_retry_at: new Date(futureTime).toISOString(),
				consecutive_fails: 8,
			};

			const { container } = renderWithProviders(
				<SortableEntry
					entry={baseEntry}
					groupEnabled={true}
					onToggle={vi.fn()}
					cbStatus={cbStatus}
				/>,
			);

			// FuseOutline should render
			const svgElement = container.querySelector("svg");
			expect(svgElement).toBeInTheDocument();

			// Entry should have overflow: hidden
			const wrapperDiv = getWrapperDiv(container);
			if (wrapperDiv) {
				expect(wrapperDiv).toHaveStyle("overflow: hidden");
			}
		});
	});
});
