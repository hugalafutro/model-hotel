import { describe, expect, it } from "vitest";
import { lazyLocaleBackend } from "../index";

// Front Desk's own backend, wired to the real catalogs: proves the glob is
// hooked up and that a language it does not ship is reported rather than
// silently answered with an empty catalog. The backend's own branches are
// covered against web-shared/i18n directly, in web/.

function read(
	language: string,
): Promise<{ err: unknown; data: object | null }> {
	return new Promise((resolve) => {
		lazyLocaleBackend.read(language, "translation", (err, data) =>
			resolve({ err, data }),
		);
	});
}

describe("lazyLocaleBackend", () => {
	it("loads a catalog lazily for a language Front Desk ships", async () => {
		const { err, data } = await read("de");
		expect(err).toBeNull();
		expect(Object.keys(data as object).length).toBeGreaterThan(0);
	});

	it("errors for a language with no catalog file", async () => {
		const { err, data } = await read("zz");
		expect(err).toBeInstanceOf(Error);
		expect((err as Error).message).toContain("zz");
		expect(data).toBeNull();
	});
});
