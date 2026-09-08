import type { DiscoveryDiff } from "../../api/types";

/** One provider's row in the discovery summary modal. */
export interface DiscoverySummaryEntry {
	providerName: string;
	diff?: DiscoveryDiff;
	error?: string;
	/** Stable React key; needed when the same provider appears more than once
	 * (e.g. several background runs recorded before review). Falls back to
	 * providerName, which is unique for a single discovery response. */
	entryKey?: string;
	/** Provider ID, when known. Enables the per-provider "Retest" action that
	 * re-runs discovery to re-probe models disabled during the original run.
	 * Background entries that only carry a provider name leave this unset. */
	providerId?: string;
}

/** The key the modal renders a row under and the retest state is matched on. */
export function entryKeyOf(entry: DiscoverySummaryEntry): string {
	return entry.entryKey ?? entry.providerName;
}
