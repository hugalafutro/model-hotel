import i18next from "i18next";
import LanguageDetector from "i18next-browser-languagedetector";
import { initReactI18next } from "react-i18next";
import en from "./locales/en.json";

// The localStorage key the language detector reads/writes. Exported as the
// single source of truth so the explicit picker write (LanguageSelector) and
// the detector's lookupLocalStorage below cannot drift apart. Kept separate
// from the main dashboard's "i18nextLng" so the two apps' language choices
// are independent.
export const LANGUAGE_STORAGE_KEY = "fdLng";

// Locale catalogs ship as one lazily-loaded chunk per language instead of
// bundling every language into the entry. English is bundled eagerly (and
// excluded from the glob) because it is the fallback language: fallback
// strings must never wait on a network fetch. Mirrors the main dashboard's
// i18n conventions on a smaller footprint.
/* v8 ignore next 4 -- bundler macro: rewritten at build time, runs at module load */
const localeLoaders = import.meta.glob<{ default: object }>([
	"./locales/*.json",
	"!./locales/en.json",
]);

const SUPPORTED_LANGUAGES = [
	"en",
	...Object.keys(localeLoaders).map((p) =>
		p.slice("./locales/".length, -".json".length),
	),
];

export function createLocaleBackend(
	loaders: Record<string, () => Promise<{ default: object }>>,
) {
	return {
		type: "backend" as const,
		init() {},
		read(
			language: string,
			_namespace: string,
			callback: (err: unknown, data: object | null) => void,
		) {
			const load = loaders[`./locales/${language}.json`];
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

export const lazyLocaleBackend = createLocaleBackend(localeLoaders);

i18next
	.use(lazyLocaleBackend)
	.use(LanguageDetector)
	.use(initReactI18next)
	.init({
		// Only English is bundled; partialBundledLanguages makes i18next pull
		// every other language from the lazy backend on demand.
		resources: { en: { translation: en } },
		partialBundledLanguages: true,
		fallbackLng: "en",
		// Catalogs are per-language, never per-region: "de-AT" → "de".
		load: "languageOnly",
		supportedLngs: SUPPORTED_LANGUAGES,
		returnEmptyString: false,
		detection: {
			// Priority: explicit user choice (localStorage) > browser/system
			// locale (navigator) > English (fallbackLng).
			order: ["localStorage", "navigator"],
			// Do NOT auto-cache the detected locale. The detected browser locale
			// must not be persisted, so changing the system language is respected
			// on every visit; only a deliberate selection in the language picker
			// writes to localStorage (see LanguageSelector in App.tsx).
			caches: [],
			lookupLocalStorage: LANGUAGE_STORAGE_KEY,
		},
		interpolation: {
			escapeValue: false,
		},
	});

export default i18next;
