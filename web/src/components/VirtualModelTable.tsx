import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { Model, ModelsCursorResponse, Provider } from "../api/types";
import { useBidirectionalFetch } from "../hooks/useBidirectionalFetch";
import { formatNumber } from "../utils/format";
import { proxyModelID } from "../utils/model";
import { ConfirmDialog } from "./ConfirmDialog";
import type { CapKey } from "./capMeta";
import { FilterDropdown } from "./FilterDropdown";
import { FilterInput } from "./FilterInput";
import { CapFilterRow } from "./modelTable/CapFilterRow";
import { ModelRow } from "./modelTable/ModelRow";
import {
	fetchModelsPage,
	type ModelSortField,
	type ModelSortState,
	modelCursor,
} from "./modelTable/modelCursor";
import { MODEL_HEADER_BASE, SortableTh } from "./modelTable/SortableTh";
import { useDeleteDisabled } from "./modelTable/useDeleteDisabled";
import { useVirtualRows } from "./modelTable/useVirtualRows";
import {
	MODEL_COL_WIDTHS_NO_PROVIDER,
	MODEL_COL_WIDTHS_WITH_PROVIDER,
} from "./modelTableWidths";

interface VirtualModelTableProps {
	providers?: Provider[];
	/** Active provider filter (provider id, "" = all). Owned by the page. */
	providerFilter?: string;
	/** When set (and providers given), renders the provider dropdown in the toolbar. */
	onProviderFilterChange?: (providerId: string) => void;
	/**
	 * Scope rows to providers with this enabled flag; undefined = any. Owned by
	 * the page, which also scopes the provider dropdown to match.
	 */
	providerEnabled?: boolean;
	onModelClick?: (model: Model) => void;
	refreshTrigger?: number;
	/** When provided, shows a "Delete disabled" button. Called with IDs of disabled models. */
	onDeleteDisabled?: (ids: string[]) => void;
	/**
	 * Called with the server's filter-wide counts whenever they change: every
	 * matching row, the rows the proxy can serve, and the rows parked under a
	 * disabled provider.
	 */
	onTotalChange?: (counts: ModelCounts) => void;
}

export interface ModelCounts {
	total: number;
	enabled: number;
	parked: number;
}

export function VirtualModelTable({
	providers,
	providerFilter = "",
	onProviderFilterChange,
	providerEnabled,
	onModelClick,
	refreshTrigger,
	onDeleteDisabled,
	onTotalChange,
}: VirtualModelTableProps) {
	"use no memo";
	const [searchQuery, setSearchQuery] = useState("");
	const [capFilter, setCapFilter] = useState<Set<CapKey>>(new Set());
	const [outputFilter, setOutputFilter] = useState<Set<string>>(new Set());
	const [sort, setSort] = useState<ModelSortState>({
		field: "name",
		dir: "asc",
	});
	const { t } = useTranslation();
	const showProviderCol = providers !== undefined;

	const toggleCapFilter = useCallback((key: CapKey) => {
		setCapFilter((prev) => {
			const next = new Set(prev);
			if (next.has(key)) next.delete(key);
			else next.add(key);
			return next;
		});
	}, []);
	const toggleOutputFilter = useCallback((key: string) => {
		setOutputFilter((prev) => {
			const next = new Set(prev);
			if (next.has(key)) next.delete(key);
			else next.add(key);
			return next;
		});
	}, []);
	const handleSort = useCallback((field: ModelSortField) => {
		setSort((prev) => ({
			field,
			dir: prev.field === field && prev.dir === "asc" ? "desc" : "asc",
		}));
	}, []);

	const filters = useMemo(() => {
		const result: Record<string, string | undefined> = {
			search: searchQuery || undefined,
			sort_by: sort.field,
		};
		if (providerFilter) {
			result.provider_id = providerFilter;
		}
		if (providerEnabled !== undefined) {
			// The cursor hook carries string filters; the client maps it back.
			result.provider_enabled = String(providerEnabled);
		}
		if (capFilter.size > 0) {
			result.capabilities = Array.from(capFilter).join(",");
		}
		if (outputFilter.size > 0) {
			result.outputs = Array.from(outputFilter).join(",");
		}
		return result;
	}, [
		searchQuery,
		sort.field,
		providerFilter,
		providerEnabled,
		capFilter,
		outputFilter,
	]);

	const getCursor = useCallback(
		(entry: Model): string => modelCursor(sort.field, entry),
		[sort.field],
	);

	const {
		entries,
		total,
		hasBefore,
		hasAfter,
		isLoadingInitial,
		isLoadingBefore,
		isLoadingAfter,
		fetchNewer,
		fetchOlder,
		reset,
		fetchInitial,
		lastResponse,
	} = useBidirectionalFetch<Model, ModelsCursorResponse>({
		fetchFn: fetchModelsPage,
		filters,
		sortDir: sort.dir,
		getCursor,
		getId: (entry) => entry.id,
	});

	// Notify parent of total count changes
	// Every page carries the same filter-wide counts, so the latest accepted
	// response is as good as any; after a reset there is none and the counts
	// fall back to zero until the refetch lands.
	const enabledTotal = lastResponse?.enabled_total ?? 0;
	const parkedTotal = lastResponse?.parked_total ?? 0;
	useEffect(() => {
		onTotalChange?.({ total, enabled: enabledTotal, parked: parkedTotal });
	}, [total, enabledTotal, parkedTotal, onTotalChange]);

	// Filter-wide, from the server: the scroller only ever holds a window of
	// rows, so counting the loaded ones would hide the button (and cap the
	// delete) at whatever happened to be scrolled in.
	const disabledCount = lastResponse?.disabled_total ?? 0;
	const {
		pendingDisabled,
		clearPendingDisabled,
		loadingDisabled,
		openDeleteDisabled,
	} = useDeleteDisabled(filters);

	// Re-fetch when parent signals data changed (e.g. after model update)
	const prevRefreshRef = useRef(refreshTrigger);
	useEffect(() => {
		if (
			refreshTrigger !== undefined &&
			refreshTrigger !== prevRefreshRef.current
		) {
			prevRefreshRef.current = refreshTrigger;
			reset();
			fetchInitial();
		}
	}, [refreshTrigger, reset, fetchInitial]);

	const {
		scrollRef,
		virtualizer,
		virtualItems,
		paddingTop,
		paddingBottom,
		handleScroll,
		startIndex,
		endIndex,
	} = useVirtualRows({
		entries,
		hasBefore,
		hasAfter,
		isLoadingBefore,
		isLoadingAfter,
		fetchNewer,
		fetchOlder,
	});

	// Render the full table (including filter controls) even when empty,
	// so users can clear/change filters when they get zero results.
	const isEmpty = entries.length === 0 && !isLoadingInitial;

	const th = (field: ModelSortField, label: string, ariaLabel: string) => (
		<SortableTh
			field={field}
			label={label}
			ariaLabel={ariaLabel}
			sort={sort}
			onSort={handleSort}
		/>
	);

	return (
		<div className="flex flex-col min-h-0">
			<div className="flex items-center gap-4 mb-4">
				<div className="flex items-center gap-2 shrink-0">
					{providers !== undefined && onProviderFilterChange && (
						<FilterDropdown
							value={providerFilter}
							onChange={onProviderFilterChange}
							placeholder={t("failover.filter_providers", {
								count: providers.length,
							})}
							allLabel={t("failover.filter_providers", {
								count: providers.length,
							})}
							options={[...providers]
								.sort((a, b) => a.name.localeCompare(b.name))
								.map((p) => ({ value: p.id, label: p.name }))}
							className="w-[220px] shrink-0"
						/>
					)}
					<FilterInput
						value={searchQuery}
						onChange={setSearchQuery}
						placeholder={t("components.virtualModelTable.searchModels")}
						className="w-[320px]"
						autoFocus
					/>
					{onDeleteDisabled && disabledCount > 0 && (
						<button
							type="button"
							onClick={openDeleteDisabled}
							disabled={loadingDisabled}
							className="ui-btn ui-btn-danger"
							aria-label={t("components.virtualModelTable.deleteDisabledAria", {
								count: disabledCount,
							})}
						>
							{t("components.virtualModelTable.deleteDisabled", {
								count: disabledCount,
							})}
						</button>
					)}
				</div>
			</div>
			<div
				ref={scrollRef}
				className="ui-card overflow-y-auto overflow-x-auto"
				style={{
					overflowAnchor: "none",
					height: "calc(100dvh - 242px)",
					minHeight: "200px",
				}}
				onScroll={handleScroll}
			>
				<table
					className="w-full table-fixed ui-table ui-table-virtual min-w-250"
					style={{
						marginTop: isEmpty ? 0 : paddingTop,
						marginBottom: isEmpty ? 8 : paddingBottom + 8,
					}}
				>
					<colgroup>
						{(showProviderCol
							? MODEL_COL_WIDTHS_WITH_PROVIDER
							: MODEL_COL_WIDTHS_NO_PROVIDER
						).map((w, i) => (
							// biome-ignore lint/suspicious/noArrayIndexKey: static col widths array, order never changes
							<col key={i} className={w} />
						))}
					</colgroup>
					<thead className="sticky top-0 z-10">
						<tr>
							{th(
								"name",
								t("models.table.model"),
								t("models.table.sortByModelName"),
							)}
							<th
								className={MODEL_HEADER_BASE}
								title={t("models.table.capabilities")}
							>
								{t("models.table.capabilities")}
							</th>
							{showProviderCol &&
								th(
									"provider",
									t("models.table.provider"),
									t("models.table.sortByProviderName"),
								)}
							{th(
								"discovered",
								t("models.table.discovered"),
								t("models.table.sortByDiscoveredDate"),
							)}
							<th aria-hidden />
							{th(
								"context",
								t("models.table.ctx"),
								t("models.table.sortByContextLength"),
							)}
							<th aria-hidden />
							{th(
								"output",
								t("models.table.maxOut"),
								t("models.table.sortByMaxOutput"),
							)}
							<th aria-hidden />
							{th(
								"status",
								t("models.table.status"),
								t("models.table.sortByStatus"),
							)}
						</tr>
						<CapFilterRow
							capFilter={capFilter}
							outputFilter={outputFilter}
							onToggleCap={toggleCapFilter}
							onToggleOutput={toggleOutputFilter}
							onClear={() => {
								setCapFilter(new Set());
								setOutputFilter(new Set());
							}}
							showProviderCol={showProviderCol}
						/>
					</thead>
					<tbody>
						{isEmpty ? (
							<tr>
								<td
									colSpan={showProviderCol ? 10 : 9}
									className="px-4 py-8 text-center text-gray-500 text-sm"
								>
									{t("components.virtualModelTable.noModelsFound")}
								</td>
							</tr>
						) : (
							virtualItems.map((vItem) => (
								<ModelRow
									key={entries[vItem.index].id}
									model={entries[vItem.index]}
									index={vItem.index}
									measureRef={virtualizer.measureElement}
									showProviderCol={showProviderCol}
									onClick={onModelClick}
								/>
							))
						)}
					</tbody>
				</table>
			</div>
			<div className="flex items-center justify-between px-3 py-2 text-xs text-gray-500 border-t border-gray-800">
				<span>
					{entries.length > 0
						? `${formatNumber(startIndex)}–${formatNumber(endIndex)} / ${formatNumber(total)}`
						: `0 / ${formatNumber(total)}`}
				</span>
				<span className="flex items-center gap-2">
					{isLoadingBefore && (
						<span className="text-(--accent)">{t("common.loadingNewer")}</span>
					)}
					{isLoadingAfter && (
						<span className="text-(--accent)">{t("common.loadingOlder")}</span>
					)}
					{isLoadingInitial && !isLoadingBefore && !isLoadingAfter && (
						<span className="text-(--accent)">{t("common.loadingDots")}</span>
					)}
				</span>
			</div>
			{pendingDisabled && onDeleteDisabled && (
				<ConfirmDialog
					title={t("components.virtualModelTable.deleteDisabledModels")}
					message={t("components.virtualModelTable.deleteDisabledMessage", {
						count: pendingDisabled.length,
					})}
					fields={pendingDisabled.map((m) =>
						proxyModelID(m.provider_name, m.model_id),
					)}
					confirmLabel={t("common.delete")}
					onConfirm={() => {
						onDeleteDisabled?.(pendingDisabled.map((m) => m.id));
						clearPendingDisabled();
					}}
					onCancel={clearPendingDisabled}
				/>
			)}
		</div>
	);
}
