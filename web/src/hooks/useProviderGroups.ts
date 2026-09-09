import { useMemo, useState } from "react";
import { toggleInSet } from "../utils/collections";

/**
 * Groups models by provider for the pickers, with the per-provider collapse
 * state beside them. `collapsed` only ever names providers currently in view,
 * so a caller never renders a chevron for a provider that is not on screen.
 * toggleCollapse leaves the absent keys in the underlying state, which costs
 * a few strings and hands a provider that leaves the list and comes back the
 * collapse the operator last chose for it; collapseAll and expandAll rewrite
 * the set from the providers on screen, so they drop that memory.
 */
export function useProviderGroups<T extends { provider_name: string }>(
	models: readonly T[],
) {
	const groups = useMemo(() => {
		const byProvider = new Map<string, T[]>();
		for (const m of models) {
			const existing = byProvider.get(m.provider_name);
			if (existing) existing.push(m);
			else byProvider.set(m.provider_name, [m]);
		}
		return byProvider;
	}, [models]);

	const [collapsedProviders, setCollapsedProviders] = useState<Set<string>>(
		new Set(),
	);

	const collapsed = useMemo(
		() => new Set([...collapsedProviders].filter((p) => groups.has(p))),
		[collapsedProviders, groups],
	);

	return {
		groups,
		collapsed,
		toggleCollapse: (provider: string) =>
			setCollapsedProviders((prev) => toggleInSet(prev, provider)),
		collapseAll: () => setCollapsedProviders(new Set(groups.keys())),
		expandAll: () => setCollapsedProviders(new Set()),
	};
}
