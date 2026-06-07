import i18next from "i18next";
import LanguageDetector from "i18next-browser-languagedetector";
import { initReactI18next } from "react-i18next";
import af from "./locales/af.json";
import ar from "./locales/ar.json";
import ca from "./locales/ca.json";
import cs from "./locales/cs.json";
import da from "./locales/da.json";
import de from "./locales/de.json";
import el from "./locales/el.json";
import en from "./locales/en.json";
import es from "./locales/es.json";
import fi from "./locales/fi.json";
import fr from "./locales/fr.json";
import he from "./locales/he.json";
import hu from "./locales/hu.json";
import it from "./locales/it.json";
import ja from "./locales/ja.json";
import ko from "./locales/ko.json";
import nl from "./locales/nl.json";
import no from "./locales/no.json";
import pl from "./locales/pl.json";
import pt from "./locales/pt.json";
import ro from "./locales/ro.json";
import ru from "./locales/ru.json";
import sk from "./locales/sk.json";
import sr from "./locales/sr.json";
import sv from "./locales/sv.json";
import tr from "./locales/tr.json";
import uk from "./locales/uk.json";
import vi from "./locales/vi.json";
import zh from "./locales/zh.json";

i18next
	.use(LanguageDetector)
	.use(initReactI18next)
	.init({
		resources: {
			af: { translation: af },
			ar: { translation: ar },
			ca: { translation: ca },
			cs: { translation: cs },
			da: { translation: da },
			de: { translation: de },
			el: { translation: el },
			en: { translation: en },
			es: { translation: es },
			fi: { translation: fi },
			fr: { translation: fr },
			he: { translation: he },
			hu: { translation: hu },
			it: { translation: it },
			ja: { translation: ja },
			ko: { translation: ko },
			nl: { translation: nl },
			no: { translation: no },
			nb: { translation: no },
			pl: { translation: pl },
			pt: { translation: pt },
			ro: { translation: ro },
			ru: { translation: ru },
			sk: { translation: sk },
			sr: { translation: sr },
			sv: { translation: sv },
			tr: { translation: tr },
			uk: { translation: uk },
			vi: { translation: vi },
			zh: { translation: zh },
		},
		fallbackLng: "en",
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
			lookupLocalStorage: "i18nextLng",
		},
		interpolation: {
			escapeValue: false,
		},
	});

export default i18next;
