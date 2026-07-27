import { createInstance } from "i18next";

// Every catalog, keyed by language code, loaded eagerly so the assertions can
// walk them rather than spelling out translated text (locale-independence
// rule): the expected string is always read from the locale file itself.
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
// applied. Comparing rendered output rather than raw templates keeps the test
// independent of how i18next is configured to interpolate.
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

// The first count in this list that selects a given category is used for it.
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
			const categories = new Intl.PluralRules(lng).resolvedOptions()
				.pluralCategories;
			for (const base of pluralBases(flat)) {
				for (const category of categories) {
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

describe("what a missing plural category does at runtime", () => {
	it("falls through to English when the locale has no form for the selected category", async () => {
		// The reason both the catalogs above and the i18n-check plural rule
		// exist. Russian count=3 asks for _few, and en cannot supply one
		// because English has no such category, so the operator reading the
		// dashboard in Russian gets the English sentence instead.
		const i18n = createInstance();
		await i18n.init({
			lng: "ru",
			fallbackLng: "en",
			interpolation: { escapeValue: false },
			resources: {
				en: {
					translation: { k_one: "{{count}} item", k_other: "{{count}} items" },
				},
				ru: {
					translation: {
						k_one: "{{count}} элемент",
						k_other: "{{count}} элемента",
					},
				},
			},
		});
		expect(i18n.t("k", { count: 1 })).toBe("1 элемент");
		expect(i18n.t("k", { count: 3 })).toBe("3 items");
	});
});
