import type { HighlighterCore } from "shiki/core";

/** Languages used by the virtual-key and model-detail snippets. */
export type SnippetLang =
	| "bash"
	| "powershell"
	| "javascript"
	| "python"
	| "json"
	| "yaml";

let highlighterPromise: Promise<HighlighterCore> | null = null;

/**
 * Lazily creates the shared snippet highlighter. Shiki core, the JavaScript
 * regex engine (no WASM), the six grammars, and the dark-plus theme all load
 * via dynamic imports, so the highlighter lands in async chunks fetched the
 * first time a snippet renders — nothing is added to the main bundle. The
 * grammars are imported individually (not via shiki's bundledLanguages map)
 * so the build emits chunks only for the languages actually used.
 */
export function getSnippetHighlighter(): Promise<HighlighterCore> {
	if (!highlighterPromise) {
		highlighterPromise = (async () => {
			const [
				{ createHighlighterCore },
				{ createJavaScriptRegexEngine },
				bash,
				powershell,
				javascript,
				python,
				json,
				yaml,
				darkPlus,
			] = await Promise.all([
				import("shiki/core"),
				import("shiki/engine/javascript"),
				import("@shikijs/langs/bash"),
				import("@shikijs/langs/powershell"),
				import("@shikijs/langs/javascript"),
				import("@shikijs/langs/python"),
				import("@shikijs/langs/json"),
				import("@shikijs/langs/yaml"),
				import("@shikijs/themes/dark-plus"),
			]);
			return createHighlighterCore({
				themes: [darkPlus.default],
				langs: [
					bash.default,
					powershell.default,
					javascript.default,
					python.default,
					json.default,
					yaml.default,
				],
				engine: createJavaScriptRegexEngine({ forgiving: true }),
			});
		})();
	}
	return highlighterPromise;
}
