import { act, render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useVirtualRows } from "../useVirtualRows";

type Row = { id: string };

function rows(from: number, to: number): Row[] {
	return Array.from({ length: to - from }, (_, i) => ({ id: `r${from + i}` }));
}

/** Renders the hook against a real scroll element whose geometry the test controls. */
function Harness({
	entries,
	hasBefore,
	hasAfter,
	fetchNewer,
	fetchOlder,
	expose,
}: {
	entries: Row[];
	hasBefore: boolean;
	hasAfter: boolean;
	fetchNewer: () => void;
	fetchOlder: () => void;
	expose: (api: { handleScroll: () => void }) => void;
}) {
	const { scrollRef, handleScroll, startIndex, endIndex } = useVirtualRows({
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
		<div ref={scrollRef} data-testid="scroller">
			<span data-testid="range">
				{startIndex}-{endIndex}
			</span>
		</div>
	);
}

function geometry(
	el: HTMLElement,
	{
		scrollTop,
		scrollHeight,
		clientHeight,
	}: { scrollTop: number; scrollHeight: number; clientHeight: number },
) {
	Object.defineProperty(el, "scrollHeight", {
		configurable: true,
		value: scrollHeight,
	});
	Object.defineProperty(el, "clientHeight", {
		configurable: true,
		value: clientHeight,
	});
	el.scrollTop = scrollTop;
}

describe("useVirtualRows", () => {
	it("keeps the viewport on the same rows when rows are prepended", () => {
		const fetchNewer = vi.fn();
		const fetchOlder = vi.fn();
		const { rerender, getByTestId } = render(
			<Harness
				entries={rows(10, 50)}
				hasBefore
				hasAfter
				fetchNewer={fetchNewer}
				fetchOlder={fetchOlder}
				expose={() => {}}
			/>,
		);
		const el = getByTestId("scroller") as HTMLDivElement;
		geometry(el, { scrollTop: 900, scrollHeight: 4000, clientHeight: 600 });

		// Ten rows land in front of the first one: the old first row keeps its
		// place on screen, so scrollTop grows by ten row heights (45px estimate
		// while nothing has been measured).
		act(() => {
			rerender(
				<Harness
					entries={[...rows(0, 10), ...rows(10, 50)]}
					hasBefore
					hasAfter
					fetchNewer={fetchNewer}
					fetchOlder={fetchOlder}
					expose={() => {}}
				/>,
			);
		});
		expect(el.scrollTop).toBe(900 + 10 * 45);
	});

	it("leaves scrollTop alone when rows are appended", () => {
		const { rerender, getByTestId } = render(
			<Harness
				entries={rows(0, 40)}
				hasBefore={false}
				hasAfter
				fetchNewer={vi.fn()}
				fetchOlder={vi.fn()}
				expose={() => {}}
			/>,
		);
		const el = getByTestId("scroller") as HTMLDivElement;
		geometry(el, { scrollTop: 300, scrollHeight: 4000, clientHeight: 600 });
		act(() => {
			rerender(
				<Harness
					entries={rows(0, 60)}
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

		geometry(el, { scrollTop: 100, scrollHeight: 4000, clientHeight: 600 });
		api.handleScroll();
		expect(fetchNewer).toHaveBeenCalledTimes(1);
		expect(fetchOlder).not.toHaveBeenCalled();

		geometry(el, { scrollTop: 3300, scrollHeight: 4000, clientHeight: 600 });
		api.handleScroll();
		expect(fetchOlder).toHaveBeenCalledTimes(1);
		expect(fetchNewer).toHaveBeenCalledTimes(1);

		// With nothing beyond either edge the same positions fetch nothing.
		rerender(
			<Harness
				entries={rows(0, 40)}
				hasBefore={false}
				hasAfter={false}
				fetchNewer={fetchNewer}
				fetchOlder={fetchOlder}
				expose={(a) => {
					api = a;
				}}
			/>,
		);
		geometry(el, { scrollTop: 100, scrollHeight: 4000, clientHeight: 600 });
		api.handleScroll();
		geometry(el, { scrollTop: 3300, scrollHeight: 4000, clientHeight: 600 });
		api.handleScroll();
		expect(fetchNewer).toHaveBeenCalledTimes(1);
		expect(fetchOlder).toHaveBeenCalledTimes(1);
	});
});
