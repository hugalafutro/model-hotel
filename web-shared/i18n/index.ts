// The lazy locale backend both frontends hand to i18next. Pure: it never
// imports i18next, because the plugin contract is structural — an object with
// `type: "backend"`, an `init` and a `read(language, namespace, callback)` — and
// the loaders map is passed in, so each app's `import.meta.glob` stays in the
// app where the bundler can rewrite it.

/** A lazily-imported catalog, keyed by the glob path both apps use. */
export type LocaleLoaders = Record<string, () => Promise<{ default: object }>>;

/**
 * createLocaleBackend wraps a map of lazy catalog imports in the shape
 * i18next's backend plugin expects, so only the fallback language has to be
 * bundled eagerly and every other one arrives as its own chunk on demand.
 *
 * `fileAliases` maps a language onto the catalog file that actually carries it,
 * for the languages that share one ("nb" reads "no.json"). A language with no
 * catalog is an error rather than a silent empty catalog, so i18next falls back
 * instead of rendering blank strings; a rejected import (a chunk that failed to
 * fetch) is passed through as the error it was.
 */
export function createLocaleBackend(
	loaders: LocaleLoaders,
	fileAliases: Record<string, string> = {},
) {
	return {
		type: "backend" as const,
		init() {},
		read(
			language: string,
			_namespace: string,
			callback: (err: unknown, data: object | null) => void,
		) {
			const file = fileAliases[language] ?? language;
			const load = loaders[`./locales/${file}.json`];
			if (!load) {
				callback(new Error(`no catalog for language "${language}"`), null);
				return;
			}
			load().then(
				(mod) => callback(null, mod.default),
				(err) => callback(err, null),
			);
		},
	};
}
