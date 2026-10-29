import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { moveOptionFocus, onActivateKey, popupKeyDown } from "../a11y";

describe("onActivateKey", () => {
	function renderTarget(fn: () => void) {
		render(
			// biome-ignore lint/a11y/useSemanticElements: the helper exists for elements that cannot be buttons
			<div role="button" tabIndex={0} onKeyDown={onActivateKey(fn)}>
				target
			</div>,
		);
		return screen.getByRole("button");
	}

	it("fires on Enter and on Space", () => {
		const fn = vi.fn();
		const el = renderTarget(fn);

		fireEvent.keyDown(el, { key: "Enter" });
		fireEvent.keyDown(el, { key: " " });

		expect(fn).toHaveBeenCalledTimes(2);
	});

	it("ignores every other key", () => {
		const fn = vi.fn();
		const el = renderTarget(fn);

		fireEvent.keyDown(el, { key: "a" });
		fireEvent.keyDown(el, { key: "Tab" });

		expect(fn).not.toHaveBeenCalled();
	});
});

describe("moveOptionFocus", () => {
	function list() {
		const ul = document.createElement("div");
		for (const id of ["a", "b", "c"]) {
			const opt = document.createElement("button");
			opt.setAttribute("role", "option");
			opt.id = id;
			ul.appendChild(opt);
		}
		document.body.appendChild(ul);
		return ul;
	}

	it("does nothing without a list", () => {
		expect(moveOptionFocus(null, "ArrowDown", null)).toBe(false);
	});

	it("leaves Home and End to a text field's caret", () => {
		const ul = list();
		const input = document.createElement("input");
		expect(moveOptionFocus(ul, "Home", input)).toBe(false);
		expect(moveOptionFocus(ul, "End", input)).toBe(false);
		const textarea = document.createElement("textarea");
		expect(moveOptionFocus(ul, "End", textarea)).toBe(false);
		ul.remove();
	});

	it("jumps to the first and last option from outside a text field", () => {
		const ul = list();
		expect(moveOptionFocus(ul, "End", null)).toBe(true);
		expect(document.activeElement?.id).toBe("c");
		expect(moveOptionFocus(ul, "Home", document.activeElement)).toBe(true);
		expect(document.activeElement?.id).toBe("a");
		ul.remove();
	});
});

describe("popupKeyDown", () => {
	it("ignores keys while the popup is closed", () => {
		const close = vi.fn();
		const trigger = document.createElement("button");
		const focus = vi.spyOn(trigger, "focus");
		const e = {
			key: "Escape",
			stopPropagation: vi.fn(),
			preventDefault: vi.fn(),
		} as unknown as Parameters<typeof popupKeyDown>[0];
		popupKeyDown(e, false, close, { current: trigger }, { current: null });
		expect(close).not.toHaveBeenCalled();
		expect(focus).not.toHaveBeenCalled();
		expect(e.stopPropagation).not.toHaveBeenCalled();
	});
});
