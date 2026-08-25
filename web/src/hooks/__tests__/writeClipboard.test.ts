import { writeClipboard } from "@web-shared/clipboard";
import { afterEach, describe, expect, it, vi } from "vitest";

// The never-throw clipboard write both frontends' useCopyToClipboard delegates
// to. Driven directly here, with no React around it, so the failure semantics
// are pinned once rather than through either hook's state.

const original = navigator.clipboard;

function stubClipboard(value: unknown) {
	Object.defineProperty(navigator, "clipboard", {
		value,
		configurable: true,
		writable: true,
	});
}

afterEach(() => {
	stubClipboard(original);
});

describe("writeClipboard", () => {
	it("writes through the Clipboard API and reports success", async () => {
		const writeText = vi.fn().mockResolvedValue(undefined);
		stubClipboard({ writeText });

		await expect(writeClipboard("hello")).resolves.toBe(true);
		expect(writeText).toHaveBeenCalledWith("hello");
	});

	it("reports failure when the clipboard refuses", async () => {
		stubClipboard({
			writeText: vi.fn().mockRejectedValue(new Error("denied")),
		});

		await expect(writeClipboard("hello")).resolves.toBe(false);
	});

	it("reports failure when there is no Clipboard API at all", async () => {
		// A non-secure (plain HTTP) context: reading .writeText off undefined
		// throws synchronously, inside the async body, so it is caught.
		stubClipboard(undefined);

		await expect(writeClipboard("hello")).resolves.toBe(false);
	});

	it("uses a caller-supplied writer instead of the Clipboard API", async () => {
		const writeText = vi.fn().mockResolvedValue(undefined);
		stubClipboard({ writeText });
		const writer = vi.fn().mockResolvedValue(undefined);

		await expect(writeClipboard("hello", writer)).resolves.toBe(true);
		expect(writer).toHaveBeenCalledWith("hello");
		expect(writeText).not.toHaveBeenCalled();
	});

	it("accepts a synchronous writer", async () => {
		const writer = vi.fn(() => {});

		await expect(writeClipboard("hello", writer)).resolves.toBe(true);
		expect(writer).toHaveBeenCalledWith("hello");
	});

	it("reports failure when a synchronous writer throws", async () => {
		const writer = vi.fn(() => {
			throw new Error("no clipboard");
		});

		await expect(writeClipboard("hello", writer)).resolves.toBe(false);
	});

	it("reports failure when an async writer rejects", async () => {
		const writer = vi.fn().mockRejectedValue(new Error("no clipboard"));

		await expect(writeClipboard("hello", writer)).resolves.toBe(false);
	});
});
