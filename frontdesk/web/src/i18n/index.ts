import i18next from "i18next";
import LanguageDetector from "i18next-browser-languagedetector";
import { initReactI18next } from "react-i18next";
import en from "./locales/en.json";

// The Front Desk UI ships English-complete in-tree; additional locale catalogs
// are a follow-up (extend `make i18n-fill`/`i18n-check` to frontdesk/web/).
// Mirrors the main app's i18n conventions on a smaller footprint: all
// user-facing strings go through t(), English is the bundled fallback.
export const LANGUAGE_STORAGE_KEY = "fdLng";

i18next
	.use(LanguageDetector)
	.use(initReactI18next)
	.init({
		resources: { en: { translation: en } },
		fallbackLng: "en",
		load: "languageOnly",
		supportedLngs: ["en"],
		returnEmptyString: false,
		detection: {
			order: ["localStorage", "navigator"],
			caches: [],
			lookupLocalStorage: LANGUAGE_STORAGE_KEY,
		},
		interpolation: { escapeValue: false },
	});

export default i18next;
