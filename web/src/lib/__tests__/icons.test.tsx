import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { BrainSlashIcon } from "../icons";

describe("BrainSlashIcon", () => {
	it("renders a Brain glyph in a box the caller's size", () => {
		const { container } = render(<BrainSlashIcon size={20} />);
		const box = container.firstElementChild as HTMLElement;
		expect(box.style.width).toBe("20px");
		expect(box.style.height).toBe("20px");
		expect(box.querySelector(".icon-brain")).not.toBeNull();
	});

	it("keeps the caller's className and defaults the size", () => {
		const { container } = render(<BrainSlashIcon className="shrink-0" />);
		const box = container.firstElementChild as HTMLElement;
		expect(box).toHaveClass("shrink-0");
		expect(box.style.width).toBe("14px");
	});
});
