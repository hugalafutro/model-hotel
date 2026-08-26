import { createLocaleBackend } from "@web-shared/i18n";
import { describe, expect, it } from "vitest";

// createLocaleBackend wraps a map of lazy locale loaders in the shape i18next's
// backend plugin expects. Exercised here against hand-written loaders, so the
// three branches of read() and the file-alias lookup are pinned without either
// app's real catalogs.

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

	it("reports an error naming the language when there is no catalog", async () => {
		const backend = createLocaleBackend({});

		const { err, data } = await readAsync(backend, "xx");

		expect(err).toBeInstanceOf(Error);
		expect((err as Error).message).toContain("xx");
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

	it("reads an aliased language out of the file that carries it", async () => {
		const catalog = { greeting: "hei" };
		const backend = createLocaleBackend(
			{ "./locales/no.json": () => Promise.resolve({ default: catalog }) },
			{ nb: "no" },
		);

		const { err, data } = await readAsync(backend, "nb");

		expect(err).toBeNull();
		expect(data).toEqual(catalog);
	});

	it("leaves an unaliased language looking for its own file", async () => {
		const backend = createLocaleBackend(
			{ "./locales/no.json": () => Promise.resolve({ default: {} }) },
			{ nb: "no" },
		);

		const { err } = await readAsync(backend, "sv");

		expect(err).toBeInstanceOf(Error);
	});
});
