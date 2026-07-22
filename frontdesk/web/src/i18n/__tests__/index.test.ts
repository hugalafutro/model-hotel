import { describe, expect, it } from "vitest";
import { createLocaleBackend } from "../index";

// createLocaleBackend wraps a map of lazy locale loaders in the shape i18next's
// backend plugin expects. Exercise read() for a present catalog, a missing one,
// and a loader that rejects — the three branches of the async read path.

function readAsync(
	backend: ReturnType<typeof createLocaleBackend>,
	language: string,
): Promise<{ err: unknown; data: object | null }> {
	return new Promise((resolve) => {
		backend.read(language, "translation", (err, data) =>
			resolve({ err, data }),
		);
	});
}

describe("createLocaleBackend", () => {
	it("resolves the loaded catalog for a known language", async () => {
		const catalog = { greeting: "hallo" };
		const backend = createLocaleBackend({
			"./locales/de.json": () => Promise.resolve({ default: catalog }),
		});

		const { err, data } = await readAsync(backend, "de");

		expect(err).toBeNull();
		expect(data).toEqual(catalog);
	});

	it("reports an error for a language with no catalog", async () => {
		const backend = createLocaleBackend({});

		const { err, data } = await readAsync(backend, "xx");

		expect(err).toBeInstanceOf(Error);
		expect(data).toBeNull();
	});

	it("propagates a rejected loader to the callback", async () => {
		const boom = new Error("chunk load failed");
		const backend = createLocaleBackend({
			"./locales/fr.json": () => Promise.reject(boom),
		});

		const { err, data } = await readAsync(backend, "fr");

		expect(err).toBe(boom);
		expect(data).toBeNull();
	});
});
