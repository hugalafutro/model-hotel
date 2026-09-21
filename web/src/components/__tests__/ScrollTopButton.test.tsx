import { fireEvent, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "../../test/utils";
import { ScrollTopButton } from "../ScrollTopButton";

/** A scroller whose viewport height and scroll position the test controls. */
function makeScroller(clientHeight: number) {
	const el = document.createElement("div");
	Object.defineProperty(el, "clientHeight", {
		configurable: true,
		value: clientHeight,
	});
	el.scrollTo = vi.fn();
	document.body.appendChild(el);
	return el;
}

function scrollTo(el: HTMLElement, top: number) {
	el.scrollTop = top;
	fireEvent.scroll(el);
}

describe("ScrollTopButton", () => {
	it("stays hidden until the scroller is past one viewport, then returns to top", () => {
		const el = makeScroller(400);
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

	it("renders nothing before the scroller mounts", () => {
		renderWithProviders(<ScrollTopButton scrollEl={null} />);
		expect(screen.queryByTestId("scroll-top-button")).toBeNull();
	});
});
