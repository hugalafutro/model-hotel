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

	// A refused write (an unfocused document, a denied permission) is where the
	// selection copy earns its keep, so it is tried there too and not only where
	// the Clipboard API is missing entirely.
	it("falls back to the legacy selection copy when the clipboard refuses", async () => {
		stubClipboard({
			writeText: vi.fn().mockRejectedValue(new Error("denied")),
		});
		const exec = vi.fn().mockReturnValue(true);
		document.execCommand = exec;

		await expect(writeClipboard("hello")).resolves.toBe(true);
		expect(exec).toHaveBeenCalledWith("copy");
		expect(document.querySelector("textarea")).toBeNull();
	});

	it("reports failure when both the clipboard and the fallback refuse", async () => {
		stubClipboard({
			writeText: vi.fn().mockRejectedValue(new Error("denied")),
		});
		document.execCommand = vi.fn().mockReturnValue(false);

		await expect(writeClipboard("hello")).resolves.toBe(false);
	});

	it("falls back to the legacy selection copy with no Clipboard API", async () => {
		// A non-secure (plain HTTP) context: a LAN dashboard served over HTTP has
		// no Clipboard API, so the selection-based copy is the only path left.
		stubClipboard(undefined);
		const exec = vi.fn().mockReturnValue(true);
		document.execCommand = exec;

		await expect(writeClipboard("hello")).resolves.toBe(true);
		expect(exec).toHaveBeenCalledWith("copy");
		// The holder textarea is removed whatever the copy reports.
		expect(document.querySelector("textarea")).toBeNull();
	});

	it("reports failure when the legacy copy is refused", async () => {
		stubClipboard(undefined);
		document.execCommand = vi.fn().mockReturnValue(false);

		await expect(writeClipboard("hello")).resolves.toBe(false);
		expect(document.querySelector("textarea")).toBeNull();
	});

	it("reports failure when the legacy copy throws", async () => {
		stubClipboard(undefined);
		document.execCommand = vi.fn(() => {
			throw new Error("unsupported");
		});

		await expect(writeClipboard("hello")).resolves.toBe(false);
		expect(document.querySelector("textarea")).toBeNull();
	});
});
