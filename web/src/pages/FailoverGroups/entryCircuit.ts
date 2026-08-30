import type { CircuitBreakerProviderStatus } from "../../api/types";

/**
 * The circuit-breaker row to render on one failover entry, or undefined when the
 * breaker is not turning that entry away.
 *
 * Circuits are keyed (provider, resolved upstream model) and the status endpoint
 * reports one row per provider, built from its most degraded circuit. So a row
 * saying "open" means *some* model of that provider is dark, not necessarily
 * this one, and painting every entry of the provider from it is exactly the
 * provider-wide darkening the per-model keying exists to end.
 *
 * An entry is turned away when the derived provider verdict is open (the breaker
 * skips the provider for every model, whether from a quota pin or from enough
 * models corroborating) or when its own model id is one the breaker is blocking.
 * A row owed a probe ("half-open") names no model at all, since open_models
 * carries only circuits that are still blocking, so it stays on every entry of
 * the provider, where it reads as recovering rather than as down.
 */
export function entryCircuitStatus(
	row: CircuitBreakerProviderStatus | undefined,
	modelId: string,
): CircuitBreakerProviderStatus | undefined {
	if (!row || row.state === "closed") return undefined;
	if (row.state === "half-open" || row.provider_open) return row;
	return row.open_models?.includes(modelId) ? row : undefined;
}
