import type { FailoverGroup } from "../../api/types";

/** The four filter controls above the list, as the page holds them. */
export interface GroupFilters {
	searchQuery: string;
	providerFilter: string;
	enabledFilter: string;
	originFilter: string;
}

export function anyFilterSet(f: GroupFilters): boolean {
	return !!(
		f.searchQuery ||
		f.providerFilter ||
		f.enabledFilter ||
		f.originFilter
	);
}

/** Groups with at least one entry on a provider whose name contains the filter. */
export function groupsMatchingProvider(
	groups: FailoverGroup[],
	providerFilter: string,
): FailoverGroup[] {
	const providerLower = providerFilter.toLowerCase();
	return groups.filter((g) =>
		g.entries.some((e) =>
			e.provider_name.toLowerCase().includes(providerLower),
		),
	);
}

export function filterGroups(
	groups: FailoverGroup[],
	f: GroupFilters,
): FailoverGroup[] {
	return groups.filter((g) => {
		const matchesModel = g.display_model
			.toLowerCase()
			.includes(f.searchQuery.toLowerCase());
		const matchesProvider =
			!f.providerFilter ||
			g.entries.some((e) =>
				e.provider_name.toLowerCase().includes(f.providerFilter.toLowerCase()),
			);
		const matchesEnabled =
			f.enabledFilter === "" ||
			(f.enabledFilter === "enabled" && g.group_enabled) ||
			(f.enabledFilter === "disabled" && !g.group_enabled);
		const matchesOrigin =
			f.originFilter === "" ||
			(f.originFilter === "auto" && g.auto_created) ||
			(f.originFilter === "manual" && !g.auto_created);
		return matchesModel && matchesProvider && matchesEnabled && matchesOrigin;
	});
}

/** Unique provider names across every group's entries, sorted, for the dropdown. */
export function providerNamesOf(groups: FailoverGroup[] | undefined): string[] {
	if (!groups) return [];
	return [
		...new Set(groups.flatMap((g) => g.entries.map((e) => e.provider_name))),
	].sort();
}

/**
 * A provider is considered disabled when it has failover entries and every
 * one of them is disabled. Derived from server data so the modal reflects
 * the real state on open (and after each toggle re-fetches the groups).
 */
export function deriveDisabledProviders(
	groups: FailoverGroup[] | undefined,
): Set<string> {
	const result = new Set<string>();
	if (!groups) return result;
	const anyEnabled = new Set<string>();
	const seen = new Set<string>();
	for (const g of groups) {
		for (const e of g.entries) {
			seen.add(e.provider_name);
			if (e.enabled) anyEnabled.add(e.provider_name);
		}
	}
	for (const name of seen) {
		if (!anyEnabled.has(name)) result.add(name);
	}
	return result;
}

/**
 * Custom groups (manually created) apart from auto groups, each sorted by
 * display model, and the auto groups bucketed by first letter for the
 * lettered sections and the alphabet sidebar.
 */
export function splitByOrigin(groups: FailoverGroup[]) {
	const byModel = (a: FailoverGroup, b: FailoverGroup) =>
		a.display_model.localeCompare(b.display_model);
	const customGroups = groups.filter((g) => !g.auto_created).sort(byModel);
	const autoGroups = groups.filter((g) => g.auto_created).sort(byModel);
	const letterGroups = autoGroups.reduce<Record<string, FailoverGroup[]>>(
		(acc, group) => {
			const letter = group.display_model.charAt(0).toUpperCase();
			if (!acc[letter]) acc[letter] = [];
			acc[letter].push(group);
			return acc;
		},
		{},
	);
	const sortedLetters = Object.keys(letterGroups).sort();
	return { customGroups, autoGroups, letterGroups, sortedLetters };
}

/**
 * A failover group needs 2+ routable members (enabled flag + live model + live
 * provider). Mirror the backend's floor in the bulk/provider toggles: count how
 * many members would remain routable after the toggle, so we disable a group
 * the moment it drops under two instead of leaving an invalid, still-enabled
 * group for the next List to silently self-heal.
 */
export function routableAfterToggle(
	group: FailoverGroup,
	entryEnabledMap: Record<string, boolean>,
): number {
	return group.entries.filter(
		(e) =>
			entryEnabledMap[e.model_uuid] && e.model_enabled && e.provider_enabled,
	).length;
}

/**
 * The update payload for a per-entry toggle. Disables a group that would drop
 * below the 2-routable-member floor, and symmetrically re-enables one that
 * regains it, so the group state matches the backend's rule immediately
 * instead of after the next List heal. One place for the rule: the bulk
 * model toggle, the bulk provider toggle and the provider modal all send it.
 */
export function entryToggleUpdate(
	group: FailoverGroup,
	entryEnabledMap: Record<string, boolean>,
): { entry_enabled: Record<string, boolean>; group_enabled?: boolean } {
	const routable = routableAfterToggle(group, entryEnabledMap);
	const alsoDisableGroup = routable < 2 && group.group_enabled;
	const alsoEnableGroup = routable >= 2 && !group.group_enabled;
	return {
		entry_enabled: entryEnabledMap,
		...(alsoDisableGroup ? { group_enabled: false } : {}),
		...(alsoEnableGroup ? { group_enabled: true } : {}),
	};
}
