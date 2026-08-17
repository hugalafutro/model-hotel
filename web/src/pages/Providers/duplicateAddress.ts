import type { Provider } from "../../api/types";

/**
 * Reduces a base URL to what actually identifies the endpoint, so two spellings
 * of the same address compare equal: case-insensitive host, no trailing slash,
 * and no `/v1` mount (the backend adds it to self-hosted providers, so a stored
 * provider and a freshly typed address differ by it more often than not).
 */
function addressKey(baseUrl: string): string {
	const trimmed = baseUrl.trim();
	if (!trimmed) return "";
	try {
		const u = new URL(trimmed);
		const path = u.pathname.replace(/\/+$/, "").replace(/\/v1$/, "");
		return `${u.protocol}//${u.host.toLowerCase()}${path}`;
	} catch {
		return trimmed.toLowerCase().replace(/\/+$/, "").replace(/\/v1$/, "");
	}
}

/**
 * Returns the provider already using this address, if any.
 *
 * Sharing an address is legitimate when the two providers carry different API
 * keys (two subscriptions on one endpoint means two quotas and real failover),
 * so this only feeds a warning. It is worth warning about because a duplicate
 * of a self-hosted server is nearly always a slip, and it quietly forms an
 * auto failover group between the two rows.
 */
export function findProviderAtAddress(
	providers: Provider[] | undefined,
	baseUrl: string,
	excludeId?: string,
): Provider | null {
	const key = addressKey(baseUrl);
	if (!key || !providers) return null;
	return (
		providers.find(
			(p) => p.id !== excludeId && addressKey(p.base_url) === key,
		) ?? null
	);
}
