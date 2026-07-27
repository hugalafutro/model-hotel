import { createInstance } from "i18next";

// Front Desk ships far fewer plural keys than the dashboard, but the same
// resolution rule applies: a category the locale omits falls through to
// English, which has no _few or _many to fall back to.
const catalogs = Object.fromEntries(
	Object.entries(
		import.meta.glob<{ default: Record<string, unknown> }>(
			"../locales/*.json",
			{
				eager: true,
			},
		),
	).map(([path, mod]) => [
		path.slice("../locales/".length, -".json".length),
		mod.default,
	]),
);

type Flat = Record<string, string>;

function flatten(obj: Record<string, unknown>, prefix = ""): Flat {
	const out: Flat = {};
	for (const [k, v] of Object.entries(obj)) {
		const path = prefix ? `${prefix}.${k}` : k;
		if (v && typeof v === "object")
			Object.assign(out, flatten(v as never, path));
		else out[path] = v as string;
	}
	return out;
}

const OTHER = "_other";
const pluralBases = (flat: Flat) =>
	Object.keys(flat)
		.filter((k) => k.endsWith(OTHER))
		.map((k) => k.slice(0, -OTHER.length));

const PLACEHOLDER = /\{\{(\w+)\}\}/g;

// Every non-count placeholder gets a marker for a value, so the rendered
// string can be compared to the catalog entry with the same substitution
// applied, without the test depending on interpolation settings.
const marker = (name: string) => `<${name}>`;

function fill(template: string, count: number) {
	const values: Record<string, string | number> = { count };
	const expected = template.replace(PLACEHOLDER, (_, name: string) => {
		if (name === "count") return String(count);
		values[name] = marker(name);
		return marker(name);
	});
	return { values, expected };
}

const CANDIDATE_COUNTS = [
	0, 1, 2, 3, 5, 7, 11, 21, 22, 25, 100, 101, 1000000, 1.5,
];

function sampleFor(lng: string, category: string): number | undefined {
	const rules = new Intl.PluralRules(lng);
	return CANDIDATE_COUNTS.find((n) => rules.select(n) === category);
}

describe("plural categories in the shipped catalogs", () => {
	it("ships every form Intl.PluralRules can select for that language", () => {
		const missing: string[] = [];
		for (const [lng, catalog] of Object.entries(catalogs)) {
			const flat = flatten(catalog);
			for (const category of new Intl.PluralRules(lng).resolvedOptions()
				.pluralCategories) {
				for (const base of pluralBases(flat)) {
					if (typeof flat[`${base}_${category}`] !== "string") {
						missing.push(`${lng}: ${base}_${category}`);
					}
				}
			}
		}
		expect(missing).toEqual([]);
	});

	it("renders the locale's own form for every category, never the English fallback", async () => {
		const i18n = createInstance();
		await i18n.init({
			lng: "en",
			fallbackLng: "en",
			interpolation: { escapeValue: false },
			resources: Object.fromEntries(
				Object.entries(catalogs).map(([lng, catalog]) => [
					lng,
					{ translation: catalog },
				]),
			),
		});

		const wrong: string[] = [];
		for (const [lng, catalog] of Object.entries(catalogs)) {
			if (lng === "en") continue;
			const flat = flatten(catalog);
			for (const category of new Intl.PluralRules(lng).resolvedOptions()
				.pluralCategories) {
				const count = sampleFor(lng, category);
				expect(
					count,
					`no candidate count selects ${category} in ${lng}`,
				).toBeDefined();
				for (const base of pluralBases(flat)) {
					const { values, expected } = fill(
						flat[`${base}_${category}`],
						count as number,
					);
					const rendered = i18n.t(base, { ...values, count, lng });
					if (rendered !== expected) {
						wrong.push(`${lng} ${base} @${count} (${category}): ${rendered}`);
					}
				}
			}
		}
		expect(wrong).toEqual([]);
	});
});
