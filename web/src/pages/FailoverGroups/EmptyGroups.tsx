import { useTranslation } from "react-i18next";
import { EmptyState } from "../../components/EmptyState";
import { anyFilterSet, type GroupFilters } from "./groupDerivations";

/**
 * The empty list, explained. Three cases: only the origin filter is set (offer
 * to create a group or clear it), some other filter hides everything (offer to
 * clear them all), or there are no groups at all (offer to auto-discover).
 */
export function EmptyGroups({
	filters,
	onCreate,
	onClearFilters,
	onSync,
}: {
	filters: GroupFilters;
	onCreate: () => void;
	onClearFilters: () => void;
	onSync: () => void;
}) {
	const { t } = useTranslation();
	const { originFilter, searchQuery, providerFilter, enabledFilter } = filters;
	if (originFilter && !searchQuery && !providerFilter && !enabledFilter) {
		return (
			<EmptyState
				message={
					originFilter === "auto"
						? t("failover.empty_no_auto")
						: t("failover.empty_no_manual")
				}
				action={{
					label:
						originFilter === "manual"
							? t("failover.empty_create_group")
							: t("failover.empty_clear_filters"),
					onClick: () =>
						originFilter === "manual" ? onCreate() : onClearFilters(),
				}}
			/>
		);
	}
	if (anyFilterSet(filters)) {
		return (
			<EmptyState
				message={t("failover.empty_no_match")}
				action={{
					label: t("failover.empty_clear_filters"),
					onClick: onClearFilters,
				}}
			/>
		);
	}
	return (
		<EmptyState
			message={t("failover.empty_no_groups")}
			action={{ label: t("failover.empty_auto_discover"), onClick: onSync }}
		/>
	);
}
