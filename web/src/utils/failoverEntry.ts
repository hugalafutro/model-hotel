/**
 * Why a failover member is N/A (not routable), as an i18n key.
 *
 * A member can be unavailable for three distinct reasons, and the operator
 * cares which one it is:
 *  - its provider is turned off,
 *  - the model was disabled by hand, or
 *  - discovery auto-disabled the model because the provider stopped offering it.
 *
 * Returns null when the member is fine (so callers can treat null as "no badge").
 * Provider-off is checked first: if the provider is down the model flag is moot.
 */
export interface NaReasonInput {
	model_enabled: boolean;
	provider_enabled: boolean;
	disabled_manually?: boolean;
}

export function naReasonKey(e: NaReasonInput): string | null {
	if (e.provider_enabled === false) {
		return "failoverGroups.entry.reasonProviderOff";
	}
	if (e.model_enabled === false) {
		return e.disabled_manually
			? "failoverGroups.entry.reasonManuallyDisabled"
			: "failoverGroups.entry.reasonRemovedByDiscovery";
	}
	return null;
}

/** True when the member is not routable (its model or provider is disabled). */
export function isNaEntry(e: NaReasonInput): boolean {
	return e.model_enabled === false || e.provider_enabled === false;
}

/**
 * The failover deletion reasons the backend emits, as i18n keys. They are
 * English sentences written by internal/failover (sync.go and revalidate.go),
 * so the mapping is on the exact strings; anything unrecognised is shown as it
 * came rather than as a missing key.
 */
const FAILOVER_DELETE_REASON_KEYS: Record<string, string> = {
	"no enabled providers found":
		"providers.discoverySummary.failoverReason.noProviders",
	"only 1 enabled provider (need 2+ for failover)":
		"providers.discoverySummary.failoverReason.onlyOne",
	"no valid providers after prune":
		"providers.discoverySummary.failoverReason.noValidProviders",
	"only 1 valid provider after prune (need 2+ for failover)":
		"providers.discoverySummary.failoverReason.onlyOneValid",
};

export function failoverDeleteReasonText(
	reason: string,
	t: (key: string) => string,
): string {
	const key = FAILOVER_DELETE_REASON_KEYS[reason];
	return key ? t(key) : reason;
}
