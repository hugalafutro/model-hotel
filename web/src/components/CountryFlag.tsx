/**
 * Maps ISO 639-1 language codes to their corresponding country flag emoji.
 *
 * Flag emojis are built from two Unicode regional indicator letters.
 * When a language maps to a specific country (e.g. "cs" → Czech Republic),
 * we use that country's flag. For languages spoken in many countries we
 * pick the most commonly associated one.
 *
 * To add a new language, add the code → flag pair here and add the locale
 * file at `web/src/i18n/locales/<code>.json`.
 */
const LANG_FLAGS: Record<string, string> = {
	en: "🇬🇧", // English → United Kingdom
	cs: "🇨🇿", // Czech → Czech Republic
	de: "🇩🇪", // German → Germany
	es: "🇪🇸", // Spanish → Spain
	fr: "🇫🇷", // French → France
	it: "🇮🇹", // Italian → Italy
	pt: "🇵🇹", // Portuguese → Portugal
	ja: "🇯🇵", // Japanese → Japan
	ko: "🇰🇷", // Korean → South Korea
	zh: "🇨🇳", // Chinese → China
	ru: "🇷🇺", // Russian → Russia
	pl: "🇵🇱", // Polish → Poland
	nl: "🇳🇱", // Dutch → Netherlands
	sv: "🇸🇪", // Swedish → Sweden
	tr: "🇹🇷", // Turkish → Turkey
	ar: "🇸🇦", // Arabic → Saudi Arabia
	hi: "🇮🇳", // Hindi → India
	th: "🇹🇭", // Thai → Thailand
	vi: "🇻🇳", // Vietnamese → Vietnam
	uk: "🇺🇦", // Ukrainian → Ukraine
	da: "🇩🇰", // Danish → Denmark
	fi: "🇫🇮", // Finnish → Finland
	no: "🇳🇴", // Norwegian → Norway
	el: "🇬🇷", // Greek → Greece
	he: "🇮🇱", // Hebrew → Israel
	id: "🇮🇩", // Indonesian → Indonesia
	ro: "🇷🇴", // Romanian → Romania
	hu: "🇭🇺", // Hungarian → Hungary
	ca: "🏴", // Catalan → Catalonia
	eo: "🌍", // Esperanto → globe (no country)
};

interface CountryFlagProps {
	/** ISO 639-1 language code */
	code: string;
	/** Additional class names */
	className?: string;
}

/**
 * Renders a country flag emoji for a given language code.
 * Falls back to a globe emoji (🌍) for unknown codes.
 */
export function CountryFlag({ code, className = "" }: CountryFlagProps) {
	const flag = LANG_FLAGS[code] ?? "🌍";
	return (
		<span
			className={`inline-block leading-none ${className}`}
			role="img"
			aria-label={`${code} flag`}
		>
			{flag}
		</span>
	);
}
