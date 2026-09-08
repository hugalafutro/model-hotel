import { describe, expect, it, vi } from "vitest";
import { autoExpandTextarea } from "../dom";

describe("autoExpandTextarea", () => {
	it("sets the height to the content's scroll height", () => {
		const el = document.createElement("textarea");
		vi.spyOn(el, "scrollHeight", "get").mockReturnValue(84);

		autoExpandTextarea(el);

		expect(el.style.height).toBe("84px");
	});
});
