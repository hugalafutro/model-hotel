import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createRef } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "../../test/utils";
import { AnchoredPopover } from "../AnchoredPopover";

// A trigger placed at a known rect, so the popover's position can be asserted
// against it rather than against jsdom's all-zero default.
function mountTrigger(rect: Partial<DOMRect>, name = "date-range") {
	const button = document.createElement("button");
	button.dataset.popoverTrigger = name;
	button.textContent = "trigger";
	document.body.appendChild(button);
	button.getBoundingClientRect = () =>
		({
			top: 0,
			left: 0,
			right: 0,
			bottom: 0,
			width: 0,
			height: 0,
			...rect,
		}) as DOMRect;
	return button;
}

afterEach(() => {
	for (const n of document.querySelectorAll("[data-popover-trigger]")) {
		n.remove();
	}
});

describe("AnchoredPopover", () => {
	it("renders its title and body into the document body", () => {
		mountTrigger({ bottom: 40, left: 100, right: 300 });
		renderWithProviders(
			<AnchoredPopover
				triggerSelector="date-range"
				title="Pick a range"
				onClose={vi.fn()}
			>
				<p>body</p>
			</AnchoredPopover>,
		);

		expect(screen.getByText("Pick a range")).toBeInTheDocument();
		expect(screen.getByText("body")).toBeInTheDocument();
	});

	it("hangs a right-anchored popover off the trigger's right edge", () => {
		mountTrigger({ bottom: 40, left: 100, right: 500 });
		renderWithProviders(
			<AnchoredPopover triggerSelector="date-range" title="t" onClose={vi.fn()}>
				<p>body</p>
			</AnchoredPopover>,
		);

		const popover = screen.getByText("body").parentElement as HTMLElement;
		// right (500) - width (288), one gap (8) below the trigger's bottom.
		expect(popover.style.left).toBe("212px");
		expect(popover.style.top).toBe("48px");
	});

	it("hangs a left-anchored popover off the trigger's left edge", () => {
		mountTrigger({ bottom: 20, left: 40, right: 500 });
		renderWithProviders(
			<AnchoredPopover
				triggerSelector="date-range"
				anchor="left"
				title="t"
				onClose={vi.fn()}
			>
				<p>body</p>
			</AnchoredPopover>,
		);

		const popover = screen.getByText("body").parentElement as HTMLElement;
		expect(popover.style.left).toBe("40px");
	});

	it("clamps a popover that would run off the right edge", () => {
		// jsdom's window is 1024 wide, so a trigger at its right edge would put
		// the popover partly off-screen.
		mountTrigger({ bottom: 10, left: 900, right: 1024 });
		renderWithProviders(
			<AnchoredPopover
				triggerSelector="date-range"
				anchor="left"
				title="t"
				onClose={vi.fn()}
			>
				<p>body</p>
			</AnchoredPopover>,
		);

		const popover = screen.getByText("body").parentElement as HTMLElement;
		expect(popover.style.left).toBe(`${window.innerWidth - 288}px`);
	});

	it("closes from the header button and from an outside click", async () => {
		const user = userEvent.setup();
		const onClose = vi.fn();
		mountTrigger({ bottom: 40, left: 100, right: 300 });
		renderWithProviders(
			<AnchoredPopover triggerSelector="date-range" title="t" onClose={onClose}>
				<p>body</p>
			</AnchoredPopover>,
		);

		await user.click(screen.getByRole("button", { name: "Close date picker" }));
		expect(onClose).toHaveBeenCalledTimes(1);

		await user.click(document.body);
		expect(onClose).toHaveBeenCalledTimes(2);
	});

	it("does not treat the trigger's own click as outside", async () => {
		const user = userEvent.setup();
		const onClose = vi.fn();
		const trigger = mountTrigger({ bottom: 40, left: 100, right: 300 });
		renderWithProviders(
			<AnchoredPopover triggerSelector="date-range" title="t" onClose={onClose}>
				<p>body</p>
			</AnchoredPopover>,
		);

		// The trigger toggles the popover itself, so closing on its click would
		// reopen-and-close on every press.
		await user.click(trigger);
		expect(onClose).not.toHaveBeenCalled();
	});

	it("renders unpositioned when its trigger is not on the page", () => {
		renderWithProviders(
			<AnchoredPopover triggerSelector="missing" title="t" onClose={vi.fn()}>
				<p>body</p>
			</AnchoredPopover>,
		);

		const popover = screen.getByText("body").parentElement as HTMLElement;
		expect(popover.style.left).toBe("0px");
		expect(popover.style.top).toBe("0px");
	});

	it("scopes the trigger lookup to triggerRef", () => {
		const scope = document.createElement("div");
		document.body.appendChild(scope);
		const inner = document.createElement("button");
		inner.dataset.popoverTrigger = "date-range";
		inner.getBoundingClientRect = () =>
			({ bottom: 60, left: 0, right: 400 }) as DOMRect;
		scope.appendChild(inner);
		// A second trigger outside the scope, which must not be the one measured.
		mountTrigger({ bottom: 999, left: 0, right: 999 });

		const ref = createRef<HTMLElement>() as React.RefObject<HTMLElement | null>;
		ref.current = scope;
		renderWithProviders(
			<AnchoredPopover
				triggerSelector="date-range"
				triggerRef={ref}
				title="t"
				onClose={vi.fn()}
			>
				<p>body</p>
			</AnchoredPopover>,
		);

		const popover = screen.getByText("body").parentElement as HTMLElement;
		expect(popover.style.top).toBe("68px");
		scope.remove();
	});
});
