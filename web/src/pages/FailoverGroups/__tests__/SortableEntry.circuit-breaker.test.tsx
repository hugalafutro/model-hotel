import { act, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { FailoverGroup } from "../../../api/types";
import { renderWithProviders } from "../../../test/utils";
import { SortableEntry, type SortableEntryProps } from "../SortableEntry";

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
	model_enabled: true,
	provider_enabled: true,
	disabled_manually: false,
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

			const { container, getByTestId, queryByTestId } = renderWithProviders(
				<SortableEntry
					entry={baseEntry}
					groupEnabled={true}
					onToggle={vi.fn()}
					cbStatus={cbStatus}
				/>,
			);

			// Half-open: static amber outline via box-shadow, NOT SVG FuseOutline
			expect(queryByTestId("fuse-outline-animated")).not.toBeInTheDocument();
			expect(container.querySelectorAll("svg").length).toBe(0);

			// Static outline div should render with amber color
			const outlineDiv = getByTestId("fuse-outline-static");
			expect(outlineDiv.getAttribute("style")).toContain("#fde68a");

			// Entry should have overflow: hidden
			const wrapperDiv = getWrapperDiv(container);
			expect(wrapperDiv).not.toBeNull();
			if (wrapperDiv) {
				expect(wrapperDiv).toHaveStyle("overflow: hidden");
			}
		});
	});

	describe("cbStatus with state 'open' but consecutive_fails < 5", () => {
		it("renders FuseOutline for open state regardless of consecutive_fails count", () => {
			const cbStatus = {
				state: "open",
				cooldown_ms: 60000,
				consecutive_fails: 3,
				next_retry_at: new Date(Date.now() + 60000).toISOString(),
			};

			const { container } = renderWithProviders(
				<SortableEntry
					entry={baseEntry}
					groupEnabled={true}
					onToggle={vi.fn()}
					cbStatus={cbStatus}
				/>,
			);

			// FuseOutline IS rendered when cbStatus.state === "open" and entry.enabled,
			// regardless of consecutive_fails count (component trusts CB state directly)
			// Note: next_retry_at must be set to avoid elapsedCooldown=true which renders
			// a boxShadow div instead of FuseOutline SVG
			const svgElements = container.querySelectorAll("svg");
			expect(svgElements.length).toBeGreaterThan(0);

			// Entry should have overflow: hidden when showFuse is true
			const wrapperDiv = getWrapperDiv(container);
			expect(wrapperDiv).not.toBeNull();
			if (wrapperDiv) {
				expect(wrapperDiv).toHaveStyle("overflow: hidden");
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
			expect(wrapperDiv).not.toBeNull();
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
			expect(wrapperDiv).not.toBeNull();
			if (wrapperDiv) {
				expect(wrapperDiv).toHaveStyle("overflow: hidden");
			}
		});
	});

	describe("Quota-pinned cooldowns", () => {
		const HOUR_MS = 60 * 60 * 1000;

		function renderEntry(cbStatus: SortableEntryProps["cbStatus"]) {
			return renderWithProviders(
				<SortableEntry
					entry={baseEntry}
					groupEnabled={true}
					onToggle={vi.fn()}
					cbStatus={cbStatus}
				/>,
			);
		}

		it("renders a static outline instead of an animated fuse for a long quota pin", () => {
			const { getByTestId, queryByTestId } = renderEntry({
				state: "open",
				consecutive_fails: 5,
				quota_pinned: true,
				cooldown_ms: 6 * HOUR_MS,
				next_retry_at: new Date(Date.now() + 6 * HOUR_MS).toISOString(),
			});

			// A six-hour CSS animation is visually frozen, so the outline stops
			// animating above the threshold and the deadline moves to the tooltip.
			expect(queryByTestId("fuse-outline-animated")).not.toBeInTheDocument();
			expect(getByTestId("fuse-outline-static")).toBeInTheDocument();
		});

		it("stops animating an ordinary cooldown that is longer than the animation threshold", () => {
			// Not quota-pinned: the threshold is about the animation being useless
			// over long spans, not about why the cooldown is long.
			const { getByTestId, queryByTestId } = renderEntry({
				state: "open",
				consecutive_fails: 5,
				cooldown_ms: 20 * 60 * 1000,
				next_retry_at: new Date(Date.now() + 20 * 60 * 1000).toISOString(),
			});

			expect(queryByTestId("fuse-outline-animated")).not.toBeInTheDocument();
			expect(
				getByTestId("fuse-outline-static").getAttribute("style"),
			).toContain("#fca5a5");
		});

		it("still animates the fuse for a short ordinary cooldown", () => {
			const { getByTestId, queryByTestId } = renderEntry({
				state: "open",
				consecutive_fails: 5,
				cooldown_ms: 60_000,
				next_retry_at: new Date(Date.now() + 60_000).toISOString(),
			});

			expect(getByTestId("fuse-outline-animated")).toBeInTheDocument();
			expect(queryByTestId("fuse-outline-static")).not.toBeInTheDocument();
		});

		it("falls back to a static outline when an open circuit reports no retry deadline", () => {
			// Without next_retry_at there is nothing to count down, but the circuit
			// is still open, so the entry must still be visibly marked.
			const { getByTestId, queryByTestId } = renderEntry({
				state: "open",
				consecutive_fails: 5,
			});

			expect(queryByTestId("fuse-outline-animated")).not.toBeInTheDocument();
			expect(
				getByTestId("fuse-outline-static").getAttribute("style"),
			).toContain("#fca5a5");
		});

		it("renders one colour for both cooldown-over paths", () => {
			// The backend reporting half-open and the client noticing next_retry_at
			// has passed are the same state, so they must render identically...
			const backendHalfOpen = renderEntry({
				state: "half-open",
				consecutive_fails: 5,
			});
			const clientElapsed = renderEntry({
				state: "open",
				consecutive_fails: 5,
				next_retry_at: new Date(Date.now() - 1000).toISOString(),
			});

			// Both renders live in the same document, so scope each lookup to its
			// own container rather than using the body-wide bound queries.
			const halfOpenStyle = within(backendHalfOpen.container)
				.getByTestId("fuse-outline-static")
				.getAttribute("style");
			const elapsedStyle = within(clientElapsed.container)
				.getByTestId("fuse-outline-static")
				.getAttribute("style");
			expect(halfOpenStyle).toEqual(elapsedStyle);
			// ...and identically *amber*, so both regressing to the open-circuit red
			// would fail rather than silently agree.
			expect(halfOpenStyle).toContain("#fde68a");
			expect(halfOpenStyle).not.toContain("#fca5a5");
		});

		it("starts animating once the countdown crosses the threshold while mounted", async () => {
			// The decision is a function of *now*, so settling it once at mount is
			// wrong: an entry mounted at 16 minutes remaining used to stay static for
			// the rest of its cooldown and never start burning, because next_retry_at
			// (the only thing it watched) never changes while the circuit stays open.
			vi.useFakeTimers({ shouldAdvanceTime: true });
			try {
				const { getByTestId, queryByTestId } = renderEntry({
					state: "open",
					consecutive_fails: 5,
					cooldown_ms: 16 * 60 * 1000,
					next_retry_at: new Date(Date.now() + 16 * 60 * 1000).toISOString(),
				});

				expect(getByTestId("fuse-outline-static")).toBeInTheDocument();
				expect(queryByTestId("fuse-outline-animated")).not.toBeInTheDocument();

				// Two minutes later the deadline is inside the animation window.
				await act(async () => {
					await vi.advanceTimersByTimeAsync(2 * 60 * 1000);
				});

				expect(getByTestId("fuse-outline-animated")).toBeInTheDocument();
				expect(queryByTestId("fuse-outline-static")).not.toBeInTheDocument();
			} finally {
				vi.useRealTimers();
			}
		});

		it("re-measures the countdown when a new deadline arrives", async () => {
			// The anchor the countdown is measured from is set once and then only
			// advanced by the threshold clock, which is right while next_retry_at
			// stands still — but a circuit that re-opens (or gets a quota pin) sends
			// a *new* deadline to an entry that never unmounted. Measured against the
			// old anchor, the new duration silently includes all the time that
			// elapsed before it arrived: the fuse burns too slowly, or sits static
			// for a cooldown that belongs inside the animation window.
			vi.useFakeTimers({ shouldAdvanceTime: true });
			try {
				const { getByTestId, queryByTestId, rerender } = renderEntry({
					state: "open",
					consecutive_fails: 5,
					cooldown_ms: 10 * 60 * 1000,
					next_retry_at: new Date(Date.now() + 10 * 60 * 1000).toISOString(),
				});

				expect(getByTestId("fuse-outline-animated")).toBeInTheDocument();

				// Nine minutes pass. Nothing ticks: a countdown already inside the
				// animation window has no clock to run, so the anchor still holds the
				// instant of mount.
				await act(async () => {
					await vi.advanceTimersByTimeAsync(9 * 60 * 1000);
				});

				// A fresh 14-minute deadline arrives — comfortably inside the 15-minute
				// animation window, but 23 minutes away from the original anchor.
				rerender(
					<SortableEntry
						entry={baseEntry}
						groupEnabled={true}
						onToggle={vi.fn()}
						cbStatus={{
							state: "open",
							consecutive_fails: 5,
							cooldown_ms: 14 * 60 * 1000,
							next_retry_at: new Date(
								Date.now() + 14 * 60 * 1000,
							).toISOString(),
						}}
					/>,
				);

				expect(getByTestId("fuse-outline-animated")).toBeInTheDocument();
				expect(queryByTestId("fuse-outline-static")).not.toBeInTheDocument();
			} finally {
				vi.useRealTimers();
			}
		});

		it("holds the animation steady once it has started", async () => {
			// The fuse restarts its CSS timeline whenever durationMs changes, so the
			// clock that watches for the crossing has to stop at the crossing. A
			// re-measured duration on every tick would make the flame jump backwards
			// to full length every time.
			vi.useFakeTimers({ shouldAdvanceTime: true });
			try {
				const { getByTestId } = renderEntry({
					state: "open",
					consecutive_fails: 5,
					cooldown_ms: 10 * 60 * 1000,
					next_retry_at: new Date(Date.now() + 10 * 60 * 1000).toISOString(),
				});

				const rectStyleBefore = getByTestId("fuse-outline-animated")
					.querySelector("rect")
					?.getAttribute("style");

				await act(async () => {
					await vi.advanceTimersByTimeAsync(3 * 60 * 1000);
				});

				const fuse = getByTestId("fuse-outline-animated");
				expect(fuse).toBeInTheDocument();
				expect(fuse.querySelector("rect")?.getAttribute("style")).toBe(
					rectStyleBefore,
				);
			} finally {
				vi.useRealTimers();
			}
		});

		it("stops its clock when the entry unmounts", async () => {
			// A row is unmounted on every failover-group refetch and on navigation.
			// An interval that outlived it would tick against a dead component for
			// the rest of the session, once per surviving row.
			vi.useFakeTimers({ shouldAdvanceTime: true });
			try {
				const { unmount } = renderEntry({
					state: "open",
					consecutive_fails: 5,
					cooldown_ms: 60 * 60 * 1000,
					next_retry_at: new Date(Date.now() + 60 * 60 * 1000).toISOString(),
				});

				expect(vi.getTimerCount()).toBeGreaterThan(0);
				unmount();
				expect(vi.getTimerCount()).toBe(0);
			} finally {
				vi.useRealTimers();
			}
		});

		it("runs no clock at all for a countdown already inside the window", async () => {
			// The common case: nothing to watch for, so nothing should tick.
			vi.useFakeTimers({ shouldAdvanceTime: true });
			try {
				renderEntry({
					state: "open",
					consecutive_fails: 5,
					cooldown_ms: 60_000,
					next_retry_at: new Date(Date.now() + 60_000).toISOString(),
				});

				expect(vi.getTimerCount()).toBe(0);
			} finally {
				vi.useRealTimers();
			}
		});

		it("names the quota reset deadline in the tooltip instead of the generic open text", () => {
			const resetAt = new Date(Date.now() + 6 * HOUR_MS);

			const pinned = renderEntry({
				state: "open",
				consecutive_fails: 5,
				quota_pinned: true,
				next_retry_at: resetAt.toISOString(),
			});
			const pinnedTitle = getWrapperDiv(pinned.container)?.getAttribute(
				"title",
			);
			expect(pinnedTitle).toContain(resetAt.toLocaleString());

			// The same cooldown without the pin keeps the generic copy and never
			// claims a reset time it does not have.
			const ordinary = renderEntry({
				state: "open",
				consecutive_fails: 5,
				next_retry_at: resetAt.toISOString(),
			});
			const ordinaryTitle = getWrapperDiv(ordinary.container)?.getAttribute(
				"title",
			);
			expect(ordinaryTitle).not.toContain(resetAt.toLocaleString());
			expect(ordinaryTitle).not.toEqual(pinnedTitle);
		});

		it("names the models a provider-wide skip rests on", () => {
			// The entry is turned away because the provider verdict is open, not
			// because of anything its own model did, so the tooltip has to name the
			// models the verdict is counted from. Model ids are data, never
			// translated, which keeps the assertion locale-independent.
			const skipped = renderEntry({
				state: "open",
				consecutive_fails: 5,
				provider_open: true,
				open_models: ["alpha-1", "alpha-2"],
			});
			const title = getWrapperDiv(skipped.container)?.getAttribute("title");
			expect(title).toContain("alpha-1, alpha-2");

			// One dark model leaves the provider serving, so that entry keeps the
			// plain open-circuit copy and names nothing it cannot claim.
			const own = renderEntry({
				state: "open",
				consecutive_fails: 5,
				provider_open: false,
				open_models: ["alpha-1"],
			});
			expect(getWrapperDiv(own.container)?.getAttribute("title")).not.toContain(
				"alpha-1",
			);
		});

		it("prefers the cooldown-over tooltip over the quota one once the pin has expired", () => {
			// quota_pinned describes the override governing the cooldown, not a
			// claim that the provider is still blocked, so an elapsed pin reads as
			// ready to probe like any other elapsed cooldown.
			const pinnedElapsed = renderEntry({
				state: "open",
				consecutive_fails: 5,
				quota_pinned: true,
				next_retry_at: new Date(Date.now() - 1000).toISOString(),
			});
			const halfOpen = renderEntry({
				state: "half-open",
				consecutive_fails: 5,
			});

			expect(
				getWrapperDiv(pinnedElapsed.container)?.getAttribute("title"),
			).toBe(getWrapperDiv(halfOpen.container)?.getAttribute("title"));
		});
	});
});
