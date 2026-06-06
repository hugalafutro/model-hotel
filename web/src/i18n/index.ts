import i18next from "i18next";
import LanguageDetector from "i18next-browser-languagedetector";
import { initReactI18next } from "react-i18next";
import cs from "./locales/cs.json";
import en from "./locales/en.json";

i18next
	.use(LanguageDetector)
	.use(initReactI18next)
	.init({
		resources: {
			cs: { translation: cs },
			en: { translation: en },
		},
		fallbackLng: "en",
		returnEmptyString: false,
		detection: {
			order: ["localStorage", "navigator"],
			caches: ["localStorage"],
			lookupLocalStorage: "i18nextLng",
		},
		interpolation: {
			escapeValue: false,
		},
	});

export default i18next;
