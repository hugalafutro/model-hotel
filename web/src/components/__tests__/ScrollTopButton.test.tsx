import { fireEvent, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "../../test/utils";
import { ScrollTopButton } from "../ScrollTopButton";

/** A scroller whose viewport height and scroll position the test controls. */
function makeScroller(clientHeight: number) {
	const el = document.createElement("div");
	let height = clientHeight;
	Object.defineProperty(el, "clientHeight", {
		configurable: true,
		get: () => height,
	});
	// Mirrors the tabIndex the real scrollers carry, so focus() takes.
	el.tabIndex = -1;
	el.scrollTo = vi.fn();
	document.body.appendChild(el);
	return {
		el,
		resizeTo(next: number) {
			height = next;
			fireEvent(window, new Event("resize"));
		},
	};
}

function scrollTo(el: HTMLElement, top: number) {
	el.scrollTop = top;
	fireEvent.scroll(el);
}

describe("ScrollTopButton", () => {
	it("stays hidden until the scroller is past one viewport, then returns to top", () => {
		const { el } = makeScroller(400);
		renderWithProviders(<ScrollTopButton scrollEl={el} />);

		expect(screen.queryByTestId("scroll-top-button")).toBeNull();

		// Exactly one viewport down is still "first page": not yet shown.
		scrollTo(el, 400);
		expect(screen.queryByTestId("scroll-top-button")).toBeNull();

		scrollTo(el, 401);
		fireEvent.click(screen.getByTestId("scroll-top-button"));
		expect(el.scrollTo).toHaveBeenCalledWith({ top: 0, behavior: "smooth" });

		// Scrolling back up hides it again.
		scrollTo(el, 0);
		expect(screen.queryByTestId("scroll-top-button")).toBeNull();
	});

	it("re-checks the threshold when the viewport resizes", () => {
		// The scroller is sized in dvh, so shrinking the window can put a
		// position that was inside the first screenful past it, with no scroll
		// event of its own to notice.
		const { el, resizeTo } = makeScroller(400);
		renderWithProviders(<ScrollTopButton scrollEl={el} />);

		scrollTo(el, 300);
		expect(screen.queryByTestId("scroll-top-button")).toBeNull();

		resizeTo(200);
		expect(screen.getByTestId("scroll-top-button")).toBeInTheDocument();
	});

	it("keeps keyboard focus on the scroller and honours reduced motion", () => {
		// Only the reduced-motion query answers "yes": ThemeContext queries the
		// colour scheme through the same stub and needs its listeners intact.
		const real = window.matchMedia;
		const reduced = vi
			.spyOn(window, "matchMedia")
			.mockImplementation((query: string) =>
				query.includes("prefers-reduced-motion")
					? ({
							matches: true,
							addEventListener: () => {},
							removeEventListener: () => {},
						} as unknown as MediaQueryList)
					: real(query),
			);
		const { el } = makeScroller(400);
		renderWithProviders(<ScrollTopButton scrollEl={el} />);

		scrollTo(el, 900);
		fireEvent.click(screen.getByTestId("scroll-top-button"));

		// The button unmounts on the way up, so focus has to land somewhere
		// other than <body>.
		expect(document.activeElement).toBe(el);
		expect(el.scrollTo).toHaveBeenCalledWith({ top: 0, behavior: "auto" });
		reduced.mockRestore();
	});

	it("renders nothing before the scroller mounts", () => {
		renderWithProviders(<ScrollTopButton scrollEl={null} />);
		expect(screen.queryByTestId("scroll-top-button")).toBeNull();
	});
});
