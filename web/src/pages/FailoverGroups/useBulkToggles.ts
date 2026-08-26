import { useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import type { FailoverGroup } from "../../api/types";
import { useToast } from "../../context/ToastContext";
import { entryToggleUpdate, groupsMatchingProvider } from "./groupDerivations";

/**
 * The three many-groups-at-once toggles: the selected groups, every group on
 * the filtered provider, and the provider modal's switch. Each writes one
 * update per group through `entryToggleUpdate` (which carries the
 * 2-routable-member rule) and re-reads the groups whether the batch
 * succeeded or not.
 */
export function useBulkToggles({
	allGroups,
	providerFilter,
	selectedGroupIds,
	clearSelection,
	refreshGroups,
}: {
	allGroups: FailoverGroup[] | undefined;
	providerFilter: string;
	selectedGroupIds: Set<string>;
	clearSelection: () => void;
	refreshGroups: () => void;
}) {
	const { toast } = useToast();
	const { t } = useTranslation();
	const [isProviderToggling, setIsProviderToggling] = useState(false);

	const handleBulkModelToggle = async (enabled: boolean) => {
		if (!allGroups) return;
		const targets = allGroups.filter((g) => selectedGroupIds.has(g.id));
		if (targets.length === 0) return;

		const promises = targets.map((group) => {
			const entryEnabledMap: Record<string, boolean> = {};
			group.entries.forEach((e) => {
				entryEnabledMap[e.model_uuid] = enabled;
			});
			return api.failoverGroups.update(
				group.id,
				entryToggleUpdate(group, entryEnabledMap),
			);
		});

		try {
			await Promise.all(promises);
			refreshGroups();
			clearSelection();
			toast(
				t("failover.toast_bulk_toggle_success", {
					action: enabled ? t("common.enabled") : t("common.disabled"),
					count: targets.length,
				}),
				"success",
			);
		} catch {
			refreshGroups();
			toast(t("failover.toast_bulk_toggle_failed"), "error");
		}
	};

	const handleBulkProviderToggle = async (enabled: boolean) => {
		if (!allGroups || !providerFilter) return;
		const providerLower = providerFilter.toLowerCase();
		const affectedGroups = groupsMatchingProvider(allGroups, providerFilter);
		if (affectedGroups.length === 0) return;

		const promises = affectedGroups.map((group) => {
			const entryEnabledMap: Record<string, boolean> = {};
			group.entries.forEach((e) => {
				entryEnabledMap[e.model_uuid] = e.provider_name
					.toLowerCase()
					.includes(providerLower)
					? enabled
					: e.enabled;
			});
			return api.failoverGroups.update(
				group.id,
				entryToggleUpdate(group, entryEnabledMap),
			);
		});

		try {
			await Promise.all(promises);
			refreshGroups();
			toast(
				t("failover.toast_provider_toggle_success", {
					action: enabled ? t("common.enabled") : t("common.disabled"),
					provider: providerFilter,
					count: affectedGroups.length,
				}),
				"success",
			);
		} catch {
			refreshGroups();
			toast(t("failover.toast_provider_toggle_failed"), "error");
		}
	};

	// Provider modal toggle
	const handleProviderToggle = async (
		providerName: string,
		enabled: boolean,
	) => {
		if (!allGroups) return;
		const affectedGroups = allGroups.filter((g) =>
			g.entries.some((e) => e.provider_name === providerName),
		);
		if (affectedGroups.length === 0) {
			toast(
				t("failover.toast_provider_toggle_no_groups", {
					provider: providerName,
				}),
				"info",
			);
			return;
		}

		setIsProviderToggling(true);
		const promises = affectedGroups.map((group) => {
			const entryEnabledMap: Record<string, boolean> = {};
			group.entries.forEach((e) => {
				entryEnabledMap[e.model_uuid] =
					e.provider_name === providerName ? enabled : e.enabled;
			});
			return api.failoverGroups.update(
				group.id,
				entryToggleUpdate(group, entryEnabledMap),
			);
		});

		try {
			await Promise.all(promises);
			// Re-fetch groups; disabledProviders is derived from the result.
			refreshGroups();
			toast(
				t("failover.toast_provider_toggle_success", {
					action: enabled ? t("common.enabled") : t("common.disabled"),
					provider: providerName,
					count: affectedGroups.length,
				}),
				"success",
			);
		} catch {
			refreshGroups();
			toast(t("failover.toast_provider_toggle_failed"), "error");
		} finally {
			setIsProviderToggling(false);
		}
	};

	return {
		handleBulkModelToggle,
		handleBulkProviderToggle,
		handleProviderToggle,
		isProviderToggling,
	};
}
