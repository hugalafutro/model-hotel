import { useTranslation } from "react-i18next";
import { FileText, ScrollText } from "@/lib/icons";
import type { DashboardUser, VirtualKey } from "../../api/types";
import { FilterDropdown } from "../../components/FilterDropdown";
import { FilterInput } from "../../components/FilterInput";
import {
	DateFilterButton,
	DateRangePickerPopover,
	ENDPOINT_FILTER_OPTIONS,
} from "../../components/logs";
import { ViewModeToggle } from "../../components/ViewModeToggle";
import type { useDateRangePicker } from "../../hooks/useDateRangePicker";

export interface RequestLogFilterValues {
	model_id: string;
	provider_id: string;
	status_code: string;
	endpoint_type: string;
	virtual_key_id: string;
	owner_user_id: string;
}

/**
 * The controls card above the request table: the Requests / Logs sub-mode
 * switch, the view-mode toggle, the filter inputs and dropdowns, and the date
 * range picker. Every filter change goes through `onFilterChange`, which the
 * page pairs with a reset to page 1.
 */
export function RequestLogFilters({
	logsSubMode,
	onSubModeChange,
	viewMode,
	onViewModeChange,
	filters,
	onFilterChange,
	keyOptions,
	ownerOptions,
	isAdmin,
	datePicker,
}: {
	logsSubMode: "request" | "app";
	onSubModeChange: (mode: "request" | "app") => void;
	viewMode: "paginate" | "scroll";
	onViewModeChange: (mode: "paginate" | "scroll") => void;
	filters: RequestLogFilterValues;
	onFilterChange: (patch: Partial<RequestLogFilterValues>) => void;
	keyOptions: VirtualKey[] | undefined;
	ownerOptions: DashboardUser[] | undefined;
	isAdmin: boolean;
	datePicker: ReturnType<typeof useDateRangePicker>;
}) {
	const { t } = useTranslation();
	const {
		dateFrom,
		dateTo,
		showDatePicker,
		pendingFrom,
		pendingTo,
		datePickerRef,
		hasDateFilter,
		pickerYear,
		pickerMonth,
		handleCalendarSelect,
		applyDateFilter,
		clearDateFilter,
		toggleDatePicker,
		closeDatePicker,
	} = datePicker;
	return (
		<div className="ui-card has-dropdown p-4 shrink-0">
			<div className="flex items-center justify-between">
				<div className="flex items-center gap-1">
					<button
						type="button"
						onClick={() => onSubModeChange("request")}
						className={`ui-btn ${
							logsSubMode === "request"
								? "ui-btn-primary ui-btn-static"
								: "ui-btn-secondary"
						}`}
					>
						<ScrollText size={12} className="inline mr-1 -mt-0.5" />
						{t("logs.tabs.requests")}
					</button>
					<button
						type="button"
						onClick={() => onSubModeChange("app")}
						className={`ui-btn ${
							logsSubMode === "app"
								? "ui-btn-primary ui-btn-static"
								: "ui-btn-secondary"
						}`}
					>
						<FileText size={12} className="inline mr-1 -mt-0.5" />
						{t("logs.tabs.logs")}
					</button>
				</div>
				<div className="flex items-center gap-2">
					<ViewModeToggle viewMode={viewMode} onChange={onViewModeChange} />
					<FilterInput
						value={filters.model_id}
						onChange={(model_id) => onFilterChange({ model_id })}
						placeholder={t("logs.filters.modelPlaceholder")}
						className="w-43"
						autoFocus
					/>
					<FilterInput
						value={filters.provider_id}
						onChange={(provider_id) => onFilterChange({ provider_id })}
						placeholder={t("logs.filters.providerPlaceholder")}
						className="w-43"
					/>
					<FilterDropdown
						value={filters.status_code}
						onChange={(status_code) => onFilterChange({ status_code })}
						placeholder={t("logs.filters.status")}
						allLabel={t("logs.filters.allStatus")}
						options={[
							{ value: "2xx", label: "2XX" },
							{ value: "4xx", label: "4XX" },
							{ value: "5xx", label: "5XX" },
							{ value: "0", label: "0" },
						]}
						className="w-31"
					/>
					<FilterDropdown
						value={filters.endpoint_type}
						onChange={(endpoint_type) => onFilterChange({ endpoint_type })}
						placeholder={t("logs.filters.endpoint")}
						allLabel={t("logs.filters.allEndpoints")}
						options={ENDPOINT_FILTER_OPTIONS.map((o) => ({
							value: o.value,
							label: t(o.labelKey),
						}))}
						className="w-38"
					/>

					{(keyOptions?.length ?? 0) > 0 && (
						<FilterDropdown
							value={filters.virtual_key_id}
							onChange={(virtual_key_id) => onFilterChange({ virtual_key_id })}
							placeholder={t("logs.filters.key")}
							allLabel={t("logs.filters.allKeys")}
							options={(keyOptions ?? []).map((k) => ({
								value: k.id,
								label: k.name,
							}))}
							className="w-28"
						/>
					)}

					{isAdmin && (ownerOptions?.length ?? 0) > 0 && (
						<FilterDropdown
							value={filters.owner_user_id}
							onChange={(owner_user_id) => onFilterChange({ owner_user_id })}
							placeholder={t("logs.filters.owner")}
							allLabel={t("logs.filters.allOwners")}
							options={(ownerOptions ?? []).map((u) => ({
								value: u.id,
								label: u.username,
							}))}
							className="w-30"
						/>
					)}

					<div className="relative" ref={datePickerRef}>
						<DateFilterButton
							hasDateFilter={hasDateFilter}
							dateFrom={dateFrom}
							dateTo={dateTo}
							onToggleDatePicker={toggleDatePicker}
							onClearDateFilter={clearDateFilter}
						/>
						{showDatePicker && (
							<DateRangePickerPopover
								pickerYear={pickerYear}
								pickerMonth={pickerMonth}
								pendingFrom={pendingFrom}
								pendingTo={pendingTo}
								onCalendarSelect={handleCalendarSelect}
								onApply={applyDateFilter}
								onClear={clearDateFilter}
								onClose={closeDatePicker}
								anchor="right"
								triggerRef={datePickerRef}
							/>
						)}
					</div>
				</div>
			</div>
		</div>
	);
}
