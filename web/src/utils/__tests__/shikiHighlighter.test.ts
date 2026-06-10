import { beforeEach, describe, expect, it, vi } from "vitest";

// Mutable switches the hoisted vi.mock factories consult so individual tests
// can make the lazy grammar/core loading fail exactly once (simulating a
// transient chunk-load failure). The failure is raised when the loaded module
// is *used*, which rejects the same cached promise a failed dynamic import
// would, exercising the eviction-and-retry path.
const importFailures = vi.hoisted(() => ({ core: false, bash: false }));

vi.mock("shiki/core", async (importOriginal) => {
	const real = await importOriginal<typeof import("shiki/core")>();
	return {
		...real,
		createHighlighterCore: ((...args) => {
			if (importFailures.core) {
				importFailures.core = false;
				throw new Error("chunk load failed: shiki/core");
			}
			return real.createHighlighterCore(...args);
		}) as typeof real.createHighlighterCore,
	};
});

vi.mock("@shikijs/langs/bash", async (importOriginal) => {
	const real = await importOriginal<typeof import("@shikijs/langs/bash")>();
	return {
		get default() {
			if (importFailures.bash) {
				importFailures.bash = false;
				throw new Error("chunk load failed: @shikijs/langs/bash");
			}
			return real.default;
		},
	};
});

/** Imports a fresh copy of the module under test so each test starts with an
 *  empty highlighter/grammar cache. */
async function freshModule() {
	vi.resetModules();
	return await import("../shikiHighlighter");
}

describe("shikiHighlighter", () => {
	beforeEach(() => {
		importFailures.core = false;
		importFailures.bash = false;
	});

	it("returns null for unsupported languages", async () => {
		const { getSnippetHighlighter } = await freshModule();
		expect(await getSnippetHighlighter("brainfuck")).toBeNull();
	});

	it("loads a grammar and tokenizes", async () => {
		const { getSnippetHighlighter } = await freshModule();
		const highlighter = await getSnippetHighlighter("bash");
		expect(highlighter).not.toBeNull();
		const tokens = highlighter?.codeToTokensBase('echo "hi"', {
			lang: "bash",
		});
		expect(tokens?.length).toBeGreaterThan(0);
	});

	it("retries a grammar load after a transient failure", async () => {
		const { getSnippetHighlighter } = await freshModule();
		importFailures.bash = true;
		await expect(getSnippetHighlighter("bash")).rejects.toThrow(
			/chunk load failed/,
		);
		// The rejected promise must have been evicted from the grammar cache,
		// so the next call retries the load and succeeds.
		const highlighter = await getSnippetHighlighter("bash");
		expect(highlighter).not.toBeNull();
		const tokens = highlighter?.codeToTokensBase('echo "hi"', {
			lang: "bash",
		});
		expect(tokens?.length).toBeGreaterThan(0);
	});

	it("retries creating the highlighter core after a transient failure", async () => {
		const { getSnippetHighlighter } = await freshModule();
		importFailures.core = true;
		await expect(getSnippetHighlighter("bash")).rejects.toThrow(
			/chunk load failed/,
		);
		const highlighter = await getSnippetHighlighter("bash");
		expect(highlighter).not.toBeNull();
	});
});
