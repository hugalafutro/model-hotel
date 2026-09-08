import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { onActivateKey } from "../a11y";

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
