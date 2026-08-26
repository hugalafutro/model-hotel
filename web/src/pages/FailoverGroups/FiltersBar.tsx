import { useTranslation } from "react-i18next";
import { CheckSquare, Square } from "@/lib/icons";
import { FilterDropdown } from "../../components/FilterDropdown";
import { FilterInput } from "../../components/FilterInput";
import type { GroupFilters } from "./groupDerivations";

/**
 * The filter row: model search, provider / state / origin dropdowns, and the
 * select-all toggle with the bulk toolbar it reveals. Selection only feeds the
 * bulk config mutations (enable / disable / delete), all of which sync
 * overwrites, so the whole select + bulk toolbar is hidden while managed.
 */
export function FiltersBar({
	filters,
	onFilterChange,
	providerNames,
	managed,
	selectedCount,
	onToggleSelectAll,
	onBulkToggle,
	onBulkDelete,
}: {
	filters: GroupFilters;
	onFilterChange: (patch: Partial<GroupFilters>) => void;
	providerNames: string[];
	managed: boolean;
	selectedCount: number;
	onToggleSelectAll: () => void;
	onBulkToggle: (enabled: boolean) => void;
	onBulkDelete: () => void;
}) {
	const { t } = useTranslation();
	return (
		<div className="flex items-center gap-3 flex-wrap">
			<FilterInput
				value={filters.searchQuery}
				onChange={(searchQuery) => onFilterChange({ searchQuery })}
				placeholder={t("failover.filter_hotel_model")}
				className="w-[260px]"
				autoFocus
			/>
			<FilterDropdown
				value={filters.providerFilter}
				onChange={(providerFilter) => onFilterChange({ providerFilter })}
				placeholder={t("failover.filter_providers", {
					count: providerNames.length,
				})}
				allLabel={t("failover.filter_providers", {
					count: providerNames.length,
				})}
				options={providerNames.map((name) => ({ value: name, label: name }))}
				className="w-[220px] shrink-0"
			/>
			<FilterDropdown
				value={filters.enabledFilter}
				onChange={(enabledFilter) => onFilterChange({ enabledFilter })}
				placeholder={t("failover.filter_states", { count: 2 })}
				allLabel={t("failover.filter_states", { count: 2 })}
				options={[
					{ value: "enabled", label: t("failover.filter_state_enabled") },
					{ value: "disabled", label: t("failover.filter_state_disabled") },
				]}
				className="w-[160px] shrink-0"
			/>
			<FilterDropdown
				value={filters.originFilter}
				onChange={(originFilter) => onFilterChange({ originFilter })}
				placeholder={t("failover.filter_origins", { count: 2 })}
				allLabel={t("failover.filter_origins", { count: 2 })}
				options={[
					{ value: "auto", label: t("failover.filter_origin_auto") },
					{ value: "manual", label: t("failover.filter_origin_manual") },
				]}
				className="w-[160px] shrink-0"
			/>
			{!managed && (
				<button
					type="button"
					onClick={onToggleSelectAll}
					className="ui-icon-btn ml-auto"
					aria-label={
						selectedCount > 0
							? t("failover.deselect_all")
							: t("failover.select_all")
					}
					title={
						selectedCount > 0
							? t("failover.deselect_all")
							: t("failover.select_all")
					}
				>
					{selectedCount > 0 ? <CheckSquare size={18} /> : <Square size={18} />}
				</button>
			)}
			{!managed && selectedCount > 0 && (
				<>
					<span className="text-sm text-gray-400">
						{t("failover.selected_count", { count: selectedCount })}
					</span>
					<button
						type="button"
						onClick={() => onBulkToggle(true)}
						className="ui-btn ui-btn-secondary"
					>
						{t("failover.btn_enable_all")}
					</button>
					<button
						type="button"
						onClick={() => onBulkToggle(false)}
						className="ui-btn ui-btn-secondary"
					>
						{t("failover.btn_disable_all")}
					</button>
					<button
						type="button"
						onClick={onBulkDelete}
						className="ui-btn ui-btn-danger"
					>
						{t("failover.btn_delete_all")}
					</button>
				</>
			)}
		</div>
	);
}
