import i18next, { lazyLocaleBackend } from "../index";

// The dashboard's own backend, wired to the real catalogs: proves the glob and
// the alias table are hooked up. The backend's own branches are covered against
// web-shared/i18n directly.

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
	it("loads a catalog lazily for a regular language", async () => {
		const { err, data } = await read("de");
		expect(err).toBeNull();
		expect(data).toBeTruthy();
		// Any real catalog has the common namespace block
		expect(Object.keys(data as object).length).toBeGreaterThan(0);
	});

	it("resolves nb through the no.json alias", async () => {
		const [nb, no] = [await read("nb"), await read("no")];
		expect(nb.err).toBeNull();
		expect(nb.data).toEqual(no.data);
	});

	it("errors for a language with no catalog file", async () => {
		const { err, data } = await read("zz");
		expect(err).toBeInstanceOf(Error);
		expect((err as Error).message).toContain("zz");
		expect(data).toBeNull();
	});
});

// The active language reaches <html>, so screen readers and the layout follow
// it on every screen: the listener sits on i18next, not on a component.
describe("document language", () => {
	afterEach(() => {
		vi.restoreAllMocks();
		return i18next.changeLanguage("en");
	});

	// A chunk that fails to load (a stale hash after a deploy) leaves the page
	// in English, so <html> must not claim a right-to-left language it is not
	// showing. i18next remembers the failure, so "he" is this test's alone.
	it("stays on the fallback when a catalog fails to load", async () => {
		vi.spyOn(lazyLocaleBackend, "read").mockImplementation((_l, _n, cb) =>
			cb(new Error("chunk 404"), null),
		);
		await i18next.changeLanguage("he");
		expect(document.documentElement.lang).toBe("en");
		expect(document.documentElement.dir).toBe("ltr");
	});

	it("sets lang and dir on <html> for each language change", async () => {
		await i18next.changeLanguage("ar");
		expect(document.documentElement.lang).toBe("ar");
		expect(document.documentElement.dir).toBe("rtl");
		await i18next.changeLanguage("de");
		expect(document.documentElement.lang).toBe("de");
		expect(document.documentElement.dir).toBe("ltr");
	});
});
