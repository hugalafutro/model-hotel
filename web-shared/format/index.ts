// The number formatters both frontends render identically: compact magnitudes
// and the units that ride on them, plus the locale every figure and date is
// formatted in. Anything whose wording depends on the active language
// (durations, relative times, count labels) stays in the app, because it needs
// i18next and this module takes no package imports: each app hands its
// language over once through setFormatLanguage.

let appLanguage: () => string = () => "en";

/** Points the formatters at the app's current language. Read at call time. */
export function setFormatLanguage(get: () => string): void {
	appLanguage = get;
}

function parseLocale(tag: string | undefined): Intl.Locale | undefined {
	try {
		return tag ? new Intl.Locale(tag) : undefined;
	} catch {
		return undefined;
	}
}

// Norwegian ships as "no" in the app while browsers report Bokmal or Nynorsk.
const sameBase = (lang: string) =>
	lang === "nb" || lang === "nn" ? "no" : lang;

/**
 * The locale every number and date is formatted in: the app language, with
 * the region of the first browser language that shares its base. App "en" with
 * browser "en-GB" formats as en-GB; app "en" with browser "de-DE" stays plain
 * "en". Browser entries keep only language, script and region, so an extension
 * or a malformed entry can never make Intl throw. Arabic keeps Latin digits
 * (some Arabic regions default to Arabic-Indic ones), as the app's other
 * figures are Latin.
 */
export function formatLocale(): string {
	const app = parseLocale(appLanguage())?.language ?? "en";
	const browser = navigator.languages?.length
		? navigator.languages
		: [navigator.language];
	const tag =
		browser
			.map(parseLocale)
			.find((l) => l && sameBase(l.language) === sameBase(app))?.baseName ??
		app;
	return app === "ar"
		? new Intl.Locale(tag, { numberingSystem: "latn" }).toString()
		: tag;
}

/**
 * A number to `digits` decimals in formatLocale(): toFixed's digits and
 * rounding, with the locale's decimal separator. `trim` drops trailing zeros
 * ("1.50" reads "1.5", "2.00" reads "2"). No digit grouping unless `grouping`
 * is set, as toFixed never groups. For display only: the output is not
 * guaranteed to parse back as a number.
 */
export function formatDecimal(
	n: number,
	digits: number,
	opts: { trim?: boolean; grouping?: boolean } = {},
): string {
	return new Intl.NumberFormat(formatLocale(), {
		minimumFractionDigits: opts.trim ? 0 : digits,
		maximumFractionDigits: digits,
		...(opts.grouping ? {} : { useGrouping: false }),
	}).format(n);
}

/** Abbreviates a number to K/M/B with at most one decimal, dropping a trailing .0. */
export function formatCompact(n: number): string {
	if (n === 0) return "0";
	const abs = Math.abs(n);
	// No grouping: the suffix already carries the magnitude ("1000K", not "1,000K").
	const fmt = (v: number) =>
		v.toLocaleString(formatLocale(), {
			maximumFractionDigits: 1,
			useGrouping: false,
		});
	if (abs >= 1_000_000_000) return `${fmt(n / 1_000_000_000)}B`;
	if (abs >= 1_000_000) return `${fmt(n / 1_000_000)}M`;
	if (abs >= 1_000) return `${fmt(n / 1_000)}K`;
	return fmt(n);
}

/** Compact token count, or "-" when the value is absent. Zero renders as "0". */
export function formatTokens(n: number | null | undefined): string {
	if (n == null) return "-";
	return formatCompact(n);
}

/** A USD amount (the gateway meters in dollars), written the locale's way. */
export function formatDollars(v: number): string {
	return v.toLocaleString(formatLocale(), {
		style: "currency",
		currency: "USD",
	});
}

/**
 * A USD spend figure: cents when the amount has them, otherwise its two
 * leading significant digits, so a $0.0000039 request does not read as $0.00.
 */
export function formatSpend(v: number): string {
	if (v === 0 || Math.abs(v) >= 0.01) return formatDollars(v);
	return v.toLocaleString(formatLocale(), {
		style: "currency",
		currency: "USD",
		maximumSignificantDigits: 2,
	});
}

/** A kWh magnitude, at most two decimals. The unit is appended by the caller. */
export function formatKwh(v: number): string {
	return v.toLocaleString(formatLocale(), { maximumFractionDigits: 2 });
}

/**
 * A whole-item count with digit grouping, or "-" when the value is absent.
 * Unlike formatTokens this does NOT abbreviate: a daily image allowance is a
 * small number the operator reads exactly, and "1.2K/1.5K" would hide the
 * difference between 1,200 and 1,249.
 */
export function formatCount(n: number | null | undefined): string {
	if (n == null) return "-";
	return Math.round(n).toLocaleString(formatLocale());
}

/** Confines a value to [lo, hi]. With an inverted range (lo > hi), hi wins. */
export function clamp(v: number, lo: number, hi: number): number {
	return Math.min(Math.max(v, lo), hi);
}
