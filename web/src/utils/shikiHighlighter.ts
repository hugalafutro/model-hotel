import type { HighlighterCore, LanguageRegistration } from "shiki/core";

/** Languages used by the virtual-key and model-detail snippets. */
export type SnippetLang =
	| "bash"
	| "powershell"
	| "javascript"
	| "python"
	| "json"
	| "yaml";

/**
 * Grammars available for highlighting, keyed by canonical shiki id. Each
 * grammar is its own dynamic import so the build emits one lazy chunk per
 * language and a grammar is only fetched when something actually renders it
 * (chat code blocks can name any of these; snippets use a fixed subset).
 */
const GRAMMAR_IMPORTS: Record<
	string,
	() => Promise<{ default: LanguageRegistration[] }>
> = {
	bash: () => import("@shikijs/langs/bash"),
	c: () => import("@shikijs/langs/c"),
	cpp: () => import("@shikijs/langs/cpp"),
	csharp: () => import("@shikijs/langs/csharp"),
	css: () => import("@shikijs/langs/css"),
	diff: () => import("@shikijs/langs/diff"),
	docker: () => import("@shikijs/langs/docker"),
	go: () => import("@shikijs/langs/go"),
	html: () => import("@shikijs/langs/html"),
	java: () => import("@shikijs/langs/java"),
	javascript: () => import("@shikijs/langs/javascript"),
	json: () => import("@shikijs/langs/json"),
	kotlin: () => import("@shikijs/langs/kotlin"),
	markdown: () => import("@shikijs/langs/markdown"),
	php: () => import("@shikijs/langs/php"),
	powershell: () => import("@shikijs/langs/powershell"),
	python: () => import("@shikijs/langs/python"),
	ruby: () => import("@shikijs/langs/ruby"),
	rust: () => import("@shikijs/langs/rust"),
	sql: () => import("@shikijs/langs/sql"),
	swift: () => import("@shikijs/langs/swift"),
	toml: () => import("@shikijs/langs/toml"),
	tsx: () => import("@shikijs/langs/tsx"),
	typescript: () => import("@shikijs/langs/typescript"),
	xml: () => import("@shikijs/langs/xml"),
	yaml: () => import("@shikijs/langs/yaml"),
};

/** Common fence-info aliases mapped to canonical grammar ids. */
const LANG_ALIASES: Record<string, string> = {
	"c#": "csharp",
	"c++": "cpp",
	cs: "csharp",
	dockerfile: "docker",
	golang: "go",
	js: "javascript",
	jsx: "javascript",
	kt: "kotlin",
	md: "markdown",
	ps: "powershell",
	ps1: "powershell",
	py: "python",
	rb: "ruby",
	rs: "rust",
	sh: "bash",
	shell: "bash",
	shellscript: "bash",
	ts: "typescript",
	yml: "yaml",
	zsh: "bash",
};

/**
 * Maps a markdown fence language string to a canonical grammar id, or null
 * when the language is not in the supported set (callers fall back to plain
 * text).
 */
export function resolveShikiLang(lang: string): string | null {
	const lower = lang.toLowerCase();
	const canonical = LANG_ALIASES[lower] ?? lower;
	return canonical in GRAMMAR_IMPORTS ? canonical : null;
}

let highlighterPromise: Promise<HighlighterCore> | null = null;
const loadedLangs = new Map<string, Promise<void>>();

function getHighlighterCore(): Promise<HighlighterCore> {
	if (!highlighterPromise) {
		highlighterPromise = (async () => {
			const [
				{ createHighlighterCore },
				{ createJavaScriptRegexEngine },
				darkPlus,
			] = await Promise.all([
				import("shiki/core"),
				import("shiki/engine/javascript"),
				import("@shikijs/themes/dark-plus"),
			]);
			return createHighlighterCore({
				themes: [darkPlus.default],
				langs: [],
				engine: createJavaScriptRegexEngine({ forgiving: true }),
			});
		})();
	}
	return highlighterPromise;
}

/**
 * Lazily creates the shared highlighter (shiki core, the JavaScript regex
 * engine — no WASM — and the dark-plus theme, all in async chunks) and loads
 * the requested grammar into it. Returns null for unsupported languages.
 */
export async function getSnippetHighlighter(
	lang: string,
): Promise<HighlighterCore | null> {
	const canonical = resolveShikiLang(lang);
	if (!canonical) return null;
	const highlighter = await getHighlighterCore();
	let loading = loadedLangs.get(canonical);
	if (!loading) {
		loading = GRAMMAR_IMPORTS[canonical]().then((mod) =>
			highlighter.loadLanguage(...mod.default),
		);
		loadedLangs.set(canonical, loading);
	}
	await loading;
	return highlighter;
}
