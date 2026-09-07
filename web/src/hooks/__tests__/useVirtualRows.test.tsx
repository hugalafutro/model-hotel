import { act, render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useVirtualRows } from "../useVirtualRows";

type Row = { id: string };

function rows(from: number, to: number): Row[] {
	return Array.from({ length: to - from }, (_, i) => ({ id: `r${from + i}` }));
}

/**
 * Real hook, real virtualizer, mocked geometry: the scroller reports a
 * 600px viewport and every mounted row reports the height `heights` assigns
 * to its id (45px when unlisted), so the virtualizer measures rows the way a
 * browser would.
 */
function Harness({
	entries,
	heights,
	hasBefore,
	hasAfter,
	fetchNewer,
	fetchOlder,
	expose,
}: {
	entries: Row[];
	heights: Record<string, number>;
	hasBefore: boolean;
	hasAfter: boolean;
	fetchNewer: () => void;
	fetchOlder: () => void;
	expose: (api: { handleScroll: () => void }) => void;
}) {
	const {
		scrollRef,
		virtualizer,
		virtualItems,
		handleScroll,
		startIndex,
		endIndex,
	} = useVirtualRows({
		entries,
		hasBefore,
		hasAfter,
		isLoadingBefore: false,
		isLoadingAfter: false,
		fetchNewer,
		fetchOlder,
	});
	expose({ handleScroll });
	return (
		<div ref={scrollRef} data-testid="scroller" style={{ height: 600 }}>
			<span data-testid="range">
				{startIndex}-{endIndex}
			</span>
			{virtualItems.map((item) => (
				<div
					key={entries[item.index].id}
					data-index={item.index}
					data-height={heights[entries[item.index].id] ?? 45}
					ref={virtualizer.measureElement}
				>
					{entries[item.index].id}
				</div>
			))}
		</div>
	);
}

/**
 * TanStack Virtual sizes the scroller and each row from offsetHeight, which
 * jsdom leaves at 0; the getter below answers with the height `data-height`
 * declares (600 for the scroller), so the real measurement path runs.
 */
function mockOffsetHeights() {
	const original = Object.getOwnPropertyDescriptor(
		HTMLElement.prototype,
		"offsetHeight",
	);
	Object.defineProperty(HTMLElement.prototype, "offsetHeight", {
		configurable: true,
		get(this: HTMLElement) {
			if (this.hasAttribute("data-height"))
				return Number(this.getAttribute("data-height"));
			if (this.getAttribute("data-testid") === "scroller") return 600;
			return 0;
		},
	});
	return () => {
		if (original)
			Object.defineProperty(HTMLElement.prototype, "offsetHeight", original);
	};
}

function scrollGeometry(
	el: HTMLElement,
	scrollTop: number,
	scrollHeight: number,
) {
	Object.defineProperty(el, "scrollHeight", {
		configurable: true,
		value: scrollHeight,
	});
	Object.defineProperty(el, "clientHeight", { configurable: true, value: 600 });
	el.scrollTop = scrollTop;
}

describe("useVirtualRows", () => {
	it("keeps the viewport on the same rows when unmeasured rows are prepended", () => {
		const { rerender, getByTestId } = render(
			<Harness
				entries={rows(10, 50)}
				heights={{}}
				hasBefore
				hasAfter
				fetchNewer={vi.fn()}
				fetchOlder={vi.fn()}
				expose={() => {}}
			/>,
		);
		const el = getByTestId("scroller") as HTMLDivElement;
		scrollGeometry(el, 900, 4000);
		// Ten rows land in front: the old first row keeps its place, so scrollTop
		// grows by ten row heights (the 45px estimate while nothing is measured).
		act(() => {
			rerender(
				<Harness
					entries={[...rows(0, 10), ...rows(10, 50)]}
					heights={{}}
					hasBefore
					hasAfter
					fetchNewer={vi.fn()}
					fetchOlder={vi.fn()}
					expose={() => {}}
				/>,
			);
		});
		expect(el.scrollTop).toBe(900 + 10 * 45);
	});

	it("uses the prepended rows' own measured heights, not the old rows' measurements", () => {
		const restore = mockOffsetHeights();
		// The two rows at the top are short and measured; the two that will be
		// prepended are tall. An index-keyed correction would shift by the short
		// rows' 20 + 30; the right answer is the tall rows' 100 + 200.
		const heights = { r10: 20, r11: 30, r8: 100, r9: 200 };
		const { rerender, getByTestId } = render(
			<Harness
				entries={rows(10, 50)}
				heights={heights}
				hasBefore
				hasAfter
				fetchNewer={vi.fn()}
				fetchOlder={vi.fn()}
				expose={() => {}}
			/>,
		);
		const el = getByTestId("scroller") as HTMLDivElement;
		scrollGeometry(el, 500, 4000);
		act(() => {
			rerender(
				<Harness
					entries={[...rows(8, 10), ...rows(10, 50)]}
					heights={heights}
					hasBefore
					hasAfter
					fetchNewer={vi.fn()}
					fetchOlder={vi.fn()}
					expose={() => {}}
				/>,
			);
		});
		expect(el.scrollTop).toBe(500 + 100 + 200);
		restore();
	});

	it("leaves scrollTop alone when rows are appended", () => {
		const { rerender, getByTestId } = render(
			<Harness
				entries={rows(0, 40)}
				heights={{}}
				hasBefore={false}
				hasAfter
				fetchNewer={vi.fn()}
				fetchOlder={vi.fn()}
				expose={() => {}}
			/>,
		);
		const el = getByTestId("scroller") as HTMLDivElement;
		scrollGeometry(el, 300, 4000);
		act(() => {
			rerender(
				<Harness
					entries={rows(0, 60)}
					heights={{}}
					hasBefore={false}
					hasAfter
					fetchNewer={vi.fn()}
					fetchOlder={vi.fn()}
					expose={() => {}}
				/>,
			);
		});
		expect(el.scrollTop).toBe(300);
	});

	it("fetches newer near the top and older near the bottom, only when there is more", () => {
		const fetchNewer = vi.fn();
		const fetchOlder = vi.fn();
		let api: { handleScroll: () => void } = { handleScroll: () => {} };
		const { getByTestId, rerender } = render(
			<Harness
				entries={rows(0, 40)}
				heights={{}}
				hasBefore
				hasAfter
				fetchNewer={fetchNewer}
				fetchOlder={fetchOlder}
				expose={(a) => {
					api = a;
				}}
			/>,
		);
		const el = getByTestId("scroller") as HTMLDivElement;

		scrollGeometry(el, 100, 4000);
		api.handleScroll();
		expect(fetchNewer).toHaveBeenCalledTimes(1);
		expect(fetchOlder).not.toHaveBeenCalled();

		scrollGeometry(el, 3300, 4000);
		api.handleScroll();
		expect(fetchOlder).toHaveBeenCalledTimes(1);
		expect(fetchNewer).toHaveBeenCalledTimes(1);

		// With nothing beyond either edge the same positions fetch nothing.
		rerender(
			<Harness
				entries={rows(0, 40)}
				heights={{}}
				hasBefore={false}
				hasAfter={false}
				fetchNewer={fetchNewer}
				fetchOlder={fetchOlder}
				expose={(a) => {
					api = a;
				}}
			/>,
		);
		scrollGeometry(el, 100, 4000);
		api.handleScroll();
		scrollGeometry(el, 3300, 4000);
		api.handleScroll();
		expect(fetchNewer).toHaveBeenCalledTimes(1);
		expect(fetchOlder).toHaveBeenCalledTimes(1);
	});
});
