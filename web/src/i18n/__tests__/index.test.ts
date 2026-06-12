import { lazyLocaleBackend } from "../index";

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
