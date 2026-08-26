// The alert event catalog's pure half, shared by both frontends: how an event
// type becomes a translation key, and how a stored selection is read back. The
// severity palette stays per app, because the two design systems name different
// colour tokens for the same four severities.

/**
 * What eventLabel needs from a translator: look a key up, fall back to the
 * supplied default. Typed structurally rather than as i18next's TFunction so
 * this module keeps to relative imports and browser globals; both apps pass
 * their own `t` straight in.
 */
export type EventTranslator = (
	key: string,
	options: { defaultValue: string },
) => string;

// eventLabel names one catalog event. The dots in an event type are not legal
// in a translation key, so they become underscores; an event the locale does
// not name yet reads as its own type rather than as a missing key, which is
// what a brand-new server-side event does before a string is added for it.
export function eventLabel(t: EventTranslator, type: string): string {
	return t(`settings.alerts.event.${type.replace(/\./g, "_")}`, {
		defaultValue: type,
	});
}

// parseCsv turns the stored alert_events CSV into a membership Set. Blank
// entries are dropped, so a trailing comma or a stray space never becomes an
// event type nothing matches.
export function parseCsv(csv: string): Set<string> {
	return new Set(
		csv
			.split(",")
			.map((s) => s.trim())
			.filter(Boolean),
	);
}
