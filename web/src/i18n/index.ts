import i18next from "i18next";
import LanguageDetector from "i18next-browser-languagedetector";
import { initReactI18next } from "react-i18next";
import en from "./locales/en.json";

// The localStorage key the language detector reads/writes. Exported as the
// single source of truth so the explicit picker write (Layout.tsx) and the
// detector's lookupLocalStorage below cannot drift apart.
export const LANGUAGE_STORAGE_KEY = "i18nextLng";

// Locale catalogs ship as one lazily-loaded chunk per language instead of
// ~3.6MB of JSON in the entry bundle. English is bundled eagerly (and
// excluded from the glob) because it is the fallback language: fallback
// strings must never wait on a network fetch.
const localeLoaders = import.meta.glob<{ default: object }>([
	"./locales/*.json",
	"!./locales/en.json",
]);

// Languages whose catalog lives in another language's file.
const FILE_ALIASES: Record<string, string> = {
	// "no" doubles as Norwegian Bokmål.
	nb: "no",
};

const SUPPORTED_LANGUAGES = [
	"en",
	...Object.keys(FILE_ALIASES),
	...Object.keys(localeLoaders).map((p) =>
		p.slice("./locales/".length, -".json".length),
	),
];

const lazyLocaleBackend = {
	type: "backend" as const,
	init() {},
	read(
		language: string,
		_namespace: string,
		callback: (err: unknown, data: object | null) => void,
	) {
		const file = FILE_ALIASES[language] ?? language;
		const load = localeLoaders[`./locales/${file}.json`];
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
			// writes to localStorage (see LanguageSelector in Layout.tsx).
			caches: [],
			lookupLocalStorage: LANGUAGE_STORAGE_KEY,
		},
		interpolation: {
			escapeValue: false,
		},
	});

export default i18next;
