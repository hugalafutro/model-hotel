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
	pinTop = false,
	listVersion = 0,
}: {
	entries: Row[];
	heights: Record<string, number>;
	hasBefore: boolean;
	hasAfter: boolean;
	fetchNewer: () => void;
	fetchOlder: () => void;
	expose: (api: { handleScroll: () => void }) => void;
	pinTop?: boolean;
	listVersion?: number;
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
		listVersion,
		hasBefore,
		hasAfter,
		isLoadingBefore: false,
		isLoadingAfter: false,
		fetchNewer,
		fetchOlder,
		pinTop,
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
	// A first page shorter than its box cannot be scrolled, so waiting for a
	// scroll would strand the rest of the list: it loads straight away.
	it.each([
		["the rows do not fill the box", 300, 1],
		["the rows overflow the box", 4000, 0],
	])(
		"pulls the next page on its own only when %s",
		(_, scrollHeight, calls) => {
			const proto = HTMLElement.prototype;
			const saved = ["clientHeight", "scrollHeight"].map(
				(k) => [k, Object.getOwnPropertyDescriptor(proto, k)] as const,
			);
			Object.defineProperty(proto, "clientHeight", {
				configurable: true,
				get: () => 600,
			});
			Object.defineProperty(proto, "scrollHeight", {
				configurable: true,
				get: () => scrollHeight,
			});
			try {
				const fetchOlder = vi.fn();
				render(
					<Harness
						entries={rows(0, 5)}
						heights={{}}
						hasBefore={false}
						hasAfter
						fetchNewer={vi.fn()}
						fetchOlder={fetchOlder}
						expose={() => {}}
					/>,
				);
				expect(fetchOlder).toHaveBeenCalledTimes(calls);
			} finally {
				for (const [k, d] of saved) {
					if (d) Object.defineProperty(proto, k, d);
					else delete (proto as unknown as Record<string, unknown>)[k];
				}
			}
		},
	);

	it("does not retry a failed fill until the rows change or the window resizes", () => {
		const proto = HTMLElement.prototype;
		const saved = ["clientHeight", "scrollHeight"].map(
			(k) => [k, Object.getOwnPropertyDescriptor(proto, k)] as const,
		);
		Object.defineProperty(proto, "clientHeight", {
			configurable: true,
			get: () => 600,
		});
		Object.defineProperty(proto, "scrollHeight", {
			configurable: true,
			get: () => 300,
		});
		try {
			const fetchOlder = vi.fn();
			const props = {
				heights: {},
				hasBefore: false,
				hasAfter: true,
				fetchNewer: vi.fn(),
				fetchOlder,
				expose: () => {},
			};
			const { rerender } = render(<Harness entries={rows(0, 5)} {...props} />);
			expect(fetchOlder).toHaveBeenCalledTimes(1);

			// The fetch failed: same rows, a fresh render, no second attempt.
			rerender(<Harness entries={rows(0, 5)} {...props} />);
			expect(fetchOlder).toHaveBeenCalledTimes(1);

			// A resize may have changed what fits: try again.
			act(() => {
				window.dispatchEvent(new Event("resize"));
			});
			expect(fetchOlder).toHaveBeenCalledTimes(2);

			// New rows arrived and still do not fill the box: next page.
			rerender(<Harness entries={rows(0, 10)} {...props} />);
			expect(fetchOlder).toHaveBeenCalledTimes(3);
		} finally {
			for (const [k, d] of saved) {
				if (d) Object.defineProperty(proto, k, d);
				else delete (proto as unknown as Record<string, unknown>)[k];
			}
		}
	});

	// A refetch swaps the whole list without emptying it first; the fetch
	// hook's listVersion, not the rows themselves, says it happened, so a new
	// list whose first row survived the filter still starts at the top.
	it.each([
		["a new list version keeps the first row", rows(10, 50), 1, 0],
		[
			"rows are appended in the same version",
			[...rows(10, 50), ...rows(50, 60)],
			0,
			900,
		],
	])(
		"when %s, scrollTop ends at the expected offset",
		(_, next, version, expected) => {
			const props = {
				heights: {},
				hasBefore: false,
				hasAfter: true,
				fetchNewer: vi.fn(),
				fetchOlder: vi.fn(),
				expose: () => {},
			};
			const { rerender, getByTestId } = render(
				<Harness entries={rows(10, 50)} listVersion={0} {...props} />,
			);
			const el = getByTestId("scroller") as HTMLDivElement;
			scrollGeometry(el, 900, 4000);
			act(() => {
				rerender(<Harness entries={next} listVersion={version} {...props} />);
			});
			expect(el.scrollTop).toBe(expected);
		},
	);

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

	// A live log list parked at the top is following the tail: correcting its
	// scroll position would push every newly prepended row straight back out of
	// view. Both log tables pass pinTop, which is what these pin.
	it.each([0, 1])(
		"leaves a pinTop list at scrollTop %i when rows are prepended",
		(top) => {
			const { rerender, getByTestId } = render(
				<Harness
					entries={rows(10, 50)}
					heights={{}}
					hasBefore
					hasAfter
					fetchNewer={vi.fn()}
					fetchOlder={vi.fn()}
					expose={() => {}}
					pinTop
				/>,
			);
			const el = getByTestId("scroller") as HTMLDivElement;
			scrollGeometry(el, top, 4000);
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
						pinTop
					/>,
				);
			});
			expect(el.scrollTop).toBe(top);
		},
	);

	// Without pinTop the top of the list is an ordinary reading position, so a
	// prepend there still holds the row the operator was looking at. This is the
	// models table, whose newest page is not a live tail.
	it("corrects a list sitting at the top when pinTop is off", () => {
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
		scrollGeometry(el, 0, 4000);
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
		expect(el.scrollTop).toBe(10 * 45);
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
