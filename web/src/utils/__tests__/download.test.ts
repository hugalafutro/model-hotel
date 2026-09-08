import { afterEach, describe, expect, it, vi } from "vitest";
import { downloadBlob } from "../download";

describe("downloadBlob", () => {
	afterEach(() => {
		vi.restoreAllMocks();
	});

	it("clicks a detached anchor and revokes the object URL", () => {
		const createObjectURL = vi.fn(() => "blob:mock");
		const revokeObjectURL = vi.fn();
		vi.stubGlobal("URL", { createObjectURL, revokeObjectURL });
		const click = vi
			.spyOn(HTMLAnchorElement.prototype, "click")
			.mockImplementation(() => {});

		downloadBlob(new Blob(["x"]), "backup.sql");

		expect(createObjectURL).toHaveBeenCalledTimes(1);
		expect(click).toHaveBeenCalledTimes(1);
		expect(revokeObjectURL).toHaveBeenCalledWith("blob:mock");
		expect(document.querySelector("a[download]")).toBeNull();

		vi.unstubAllGlobals();
	});
});
