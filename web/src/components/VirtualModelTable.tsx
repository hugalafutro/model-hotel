import { useVirtualizer } from "@tanstack/react-virtual";
import {
	useCallback,
	useEffect,
	useLayoutEffect,
	useMemo,
	useRef,
	useState,
} from "react";
import { useTranslation } from "react-i18next";
import { api } from "../api/client";
import type { Model, ModelsCursorResponse, Provider } from "../api/types";
import { useBidirectionalFetch } from "../hooks/useBidirectionalFetch";
import {
	encodeCursor,
	formatDate,
	formatNumber,
	formatRelativeTime,
} from "../utils/format";
import { parseCapabilities, proxyModelID } from "../utils/model";
import { ConfirmDialog } from "./ConfirmDialog";
import { CopyablePill } from "./CopyablePill";
import { CAP_META, type CapKey, hasCap } from "./capMeta";
import { FilterDropdown } from "./FilterDropdown";
import { FilterInput } from "./FilterInput";
import {
	MODEL_COL_WIDTHS_NO_PROVIDER,
	MODEL_COL_WIDTHS_WITH_PROVIDER,
} from "./modelTableWidths";
import { OutputBadges } from "./OutputBadges";
import { OUTPUT_META } from "./outputMeta";

interface VirtualModelTableProps {
	providers?: Provider[];
	/** Active provider filter (provider id, "" = all). Owned by the page. */
	providerFilter?: string;
	/** When set (and providers given), renders the provider dropdown in the toolbar. */
	onProviderFilterChange?: (providerId: string) => void;
	onModelClick?: (model: Model) => void;
	refreshTrigger?: number;
	/** When provided, shows a "Delete disabled" button. Called with IDs of disabled models. */
	onDeleteDisabled?: (ids: string[]) => void;
	/** Called with the total model count from the server whenever it changes. */
	onTotalChange?: (total: number) => void;
}

interface SortState {
	field: "name" | "discovered" | "context" | "output" | "provider" | "status";
	dir: "asc" | "desc";
}

const HEADER_BASE =
	"px-4 py-2 text-left text-xs font-medium uppercase tracking-wider whitespace-nowrap ui-table-header-text";

const EDGE_THRESHOLD_PX = 500;

export function VirtualModelTable({
	providers,
	providerFilter = "",
	onProviderFilterChange,
	onModelClick,
	refreshTrigger,
	onDeleteDisabled,
	onTotalChange,
}: VirtualModelTableProps) {
	"use no memo";
	const [searchQuery, setSearchQuery] = useState("");
	const [capFilter, setCapFilter] = useState<Set<CapKey>>(new Set());
	const [sort, setSort] = useState<SortState>({
		field: "name",
		dir: "asc",
	});
	const { t } = useTranslation();
	const [confirmDeleteDisabled, setConfirmDeleteDisabled] = useState(false);

	const scrollRef = useRef<HTMLDivElement>(null);

	const showProviderCol = providers !== undefined;

	const toggleCapFilter = useCallback((key: CapKey) => {
		setCapFilter((prev) => {
			const next = new Set(prev);
			if (next.has(key)) next.delete(key);
			else next.add(key);
			return next;
		});
	}, []);

	const [outputFilter, setOutputFilter] = useState<Set<string>>(new Set());
	const toggleOutputFilter = useCallback((key: string) => {
		setOutputFilter((prev) => {
			const next = new Set(prev);
			if (next.has(key)) next.delete(key);
			else next.add(key);
			return next;
		});
	}, []);

	const handleSort = useCallback((field: SortState["field"]) => {
		setSort((prev) => ({
			field,
			dir: prev.field === field && prev.dir === "asc" ? "desc" : "asc",
		}));
	}, []);

	const fetchFn = useCallback(
		async (params: {
			cursor?: string;
			direction: "after" | "before";
			limit: number;
			sort_dir: string;
			[key: string]: string | number | undefined;
		}): Promise<ModelsCursorResponse> => {
			return api.models.cursor({
				cursor: params.cursor,
				direction: params.direction as "after" | "before",
				limit: params.limit,
				sort_by: params.sort_by as string | undefined,
				sort_dir: params.sort_dir,
				provider_id: params.provider_id as string | undefined,
				search: params.search as string | undefined,
				capabilities: params.capabilities as string | undefined,
				outputs: params.outputs as string | undefined,
			});
		},
		[],
	);

	const filters = useMemo(() => {
		const result: Record<string, string | undefined> = {
			search: searchQuery || undefined,
			sort_by: sort.field,
		};
		if (providerFilter) {
			result.provider_id = providerFilter;
		}
		if (capFilter.size > 0) {
			result.capabilities = Array.from(capFilter).join(",");
		}
		if (outputFilter.size > 0) {
			result.outputs = Array.from(outputFilter).join(",");
		}
		return result;
	}, [searchQuery, sort.field, providerFilter, capFilter, outputFilter]);

	const getCursor = useCallback(
		(entry: Model): string => {
			let cursorObj: Record<string, unknown>;
			switch (sort.field) {
				case "name":
					cursorObj = {
						sort_by: "name",
						name: entry.name || entry.model_id,
						model_id: entry.model_id,
						id: entry.id,
					};
					break;
				case "discovered":
					cursorObj = {
						sort_by: "discovered",
						last_seen_at: entry.last_seen_at,
						id: entry.id,
					};
					break;
				case "context":
					cursorObj = {
						sort_by: "context",
						context_length: entry.context_length ?? 0,
						id: entry.id,
					};
					break;
				case "output":
					cursorObj = {
						sort_by: "output",
						max_output_tokens: entry.max_output_tokens ?? 0,
						id: entry.id,
					};
					break;
				case "provider":
					cursorObj = {
						sort_by: "provider",
						provider_name: entry.provider_name,
						id: entry.id,
					};
					break;
				case "status":
					cursorObj = {
						sort_by: "status",
						status_sort: entry.enabled ? (entry.disabled_manually ? 1 : 0) : 2,
						id: entry.id,
					};
					break;
				default:
					cursorObj = { sort_by: "name", name: entry.name, id: entry.id };
			}
			return encodeCursor(cursorObj);
		},
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
	} = useBidirectionalFetch<Model>({
		fetchFn,
		filters,
		sortDir: sort.dir,
		getCursor,
		getId: (entry) => entry.id,
	});

	// Notify parent of total count changes
	useEffect(() => {
		onTotalChange?.(total);
	}, [total, onTotalChange]);

	const existingCaps = useMemo(() => {
		const caps = new Set<CapKey>();
		entries.forEach((m) => {
			const c = parseCapabilities(m.capabilities);
			CAP_META.forEach((meta) => {
				if (hasCap(c, meta.key)) caps.add(meta.key);
			});
		});
		return caps;
	}, [entries]);

	const disabledModels = useMemo(
		() => entries.filter((m) => !m.enabled),
		[entries],
	);
	const disabledModelIds = useMemo(
		() => disabledModels.map((m) => m.id),
		[disabledModels],
	);
	const disabledCount = disabledModelIds.length;

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

	// eslint-disable-next-line react-hooks/incompatible-library -- TanStack Virtual returns mutable functions; compiler skips memoization
	const virtualizer = useVirtualizer({
		count: entries.length,
		getScrollElement: () => scrollRef.current,
		estimateSize: () => 45,
		overscan: 20,
	});

	const virtualItems = virtualizer.getVirtualItems();

	const prevEntriesRef = useRef(entries);
	// State counter to force synchronous re-render after scrollTop adjustment.
	// React guarantees setState inside useLayoutEffect is flushed before paint.
	const [, forceRerender] = useState(0);

	// When items are prepended (fetchNewer), all item indices shift but
	// scrollTop stays the same, so the virtualizer maps the old scroll
	// position to different items. Adjust scrollTop by the average of
	// the virtualizer's measured row sizes (from measureElement /
	// ResizeObserver), falling back to estimateSize when no measurements
	// exist yet. Then force a synchronous re-render so the virtualizer
	// recomputes before the browser paints.
	useLayoutEffect(() => {
		const prev = prevEntriesRef.current;
		if (entries.length > prev.length && prev.length > 0) {
			const newItemCount = entries.length - prev.length;
			if (entries[newItemCount]?.id === prev[0]?.id && scrollRef.current) {
				const cache = virtualizer.measurementsCache;
				const avgSize =
					cache.length > 0
						? cache.reduce((sum, m) => sum + m.size, 0) / cache.length
						: 45;
				scrollRef.current.scrollTop += newItemCount * avgSize;
				prevEntriesRef.current = entries;
				forceRerender((c) => c + 1);
				return;
			}
		}
		prevEntriesRef.current = entries;
	}, [entries, virtualizer.measurementsCache]);

	const [paddingTop, paddingBottom] =
		virtualItems.length > 0
			? [
					Math.max(0, virtualItems[0].start),
					Math.max(
						0,
						virtualizer.getTotalSize() -
							virtualItems[virtualItems.length - 1].end,
					),
				]
			: [0, 0];

	const handleScroll = useCallback(() => {
		const el = scrollRef.current;
		if (!el) return;

		const nearTop = el.scrollTop < EDGE_THRESHOLD_PX;
		const nearBottom =
			el.scrollHeight - el.scrollTop - el.clientHeight < EDGE_THRESHOLD_PX;

		if (nearTop && hasBefore && !isLoadingBefore) {
			fetchNewer();
		}
		if (nearBottom && hasAfter && !isLoadingAfter) {
			fetchOlder();
		}
	}, [
		hasBefore,
		hasAfter,
		isLoadingBefore,
		isLoadingAfter,
		fetchNewer,
		fetchOlder,
	]);

	const startIndex = virtualItems.length > 0 ? virtualItems[0].index + 1 : 0;
	const endIndex =
		virtualItems.length > 0
			? virtualItems[virtualItems.length - 1].index + 1
			: 0;

	// Render the full table (including filter controls) even when empty,
	// so users can clear/change filters when they get zero results.
	const isEmpty = entries.length === 0 && !isLoadingInitial;

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
							onClick={() => setConfirmDeleteDisabled(true)}
							className="ui-btn ui-btn-danger flex items-center gap-1 px-2 py-1 text-xs"
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
							<th
								className={`${HEADER_BASE} cursor-pointer select-none hover:text-gray-200`}
								onClick={() => handleSort("name")}
								title={t("models.table.model")}
							>
								<button
									type="button"
									className=""
									aria-label={t("models.table.sortByModelName")}
								>
									{t("models.table.model")}{" "}
									<span className="inline-block w-3 text-center">
										{sort.field === "name"
											? sort.dir === "asc"
												? "↑"
												: "↓"
											: " "}
									</span>
								</button>
							</th>
							<th
								className={HEADER_BASE}
								title={t("models.table.capabilities")}
							>
								{t("models.table.capabilities")}
							</th>
							{showProviderCol && (
								<th
									className={`${HEADER_BASE} cursor-pointer select-none hover:text-gray-200`}
									onClick={() => handleSort("provider")}
									title={t("models.table.provider")}
								>
									<button
										type="button"
										className=""
										aria-label={t("models.table.sortByProviderName")}
									>
										{t("models.table.provider")}{" "}
										<span className="inline-block w-3 text-center">
											{sort.field === "provider"
												? sort.dir === "asc"
													? "↑"
													: "↓"
												: " "}
										</span>
									</button>
								</th>
							)}
							<th
								className={`${HEADER_BASE} cursor-pointer select-none hover:text-gray-200`}
								onClick={() => handleSort("discovered")}
								title={t("models.table.discovered")}
							>
								<button
									type="button"
									className=""
									aria-label={t("models.table.sortByDiscoveredDate")}
								>
									{t("models.table.discovered")}{" "}
									<span className="inline-block w-3 text-center">
										{sort.field === "discovered"
											? sort.dir === "asc"
												? "↑"
												: "↓"
											: " "}
									</span>
								</button>
							</th>
							<th aria-hidden />
							<th
								className={`${HEADER_BASE} cursor-pointer select-none hover:text-gray-200`}
								onClick={() => handleSort("context")}
								title={t("models.table.ctx")}
							>
								<button
									type="button"
									className=""
									aria-label={t("models.table.sortByContextLength")}
								>
									{t("models.table.ctx")}{" "}
									<span className="inline-block w-3 text-center">
										{sort.field === "context"
											? sort.dir === "asc"
												? "↑"
												: "↓"
											: " "}
									</span>
								</button>
							</th>
							<th aria-hidden />
							<th
								className={`${HEADER_BASE} cursor-pointer select-none hover:text-gray-200`}
								onClick={() => handleSort("output")}
								title={t("models.table.maxOut")}
							>
								<button
									type="button"
									className=""
									aria-label={t("models.table.sortByMaxOutput")}
								>
									{t("models.table.maxOut")}{" "}
									<span className="inline-block w-3 text-center">
										{sort.field === "output"
											? sort.dir === "asc"
												? "↑"
												: "↓"
											: " "}
									</span>
								</button>
							</th>
							<th aria-hidden />
							<th
								className={`${HEADER_BASE} cursor-pointer select-none hover:text-gray-200`}
								onClick={() => handleSort("status")}
								title={t("models.table.status")}
							>
								<button
									type="button"
									className=""
									aria-label={t("models.table.sortByStatus")}
								>
									{t("models.table.status")}{" "}
									<span className="inline-block w-3 text-center">
										{sort.field === "status"
											? sort.dir === "asc"
												? "↑"
												: "↓"
											: " "}
									</span>
								</button>
							</th>
						</tr>
						<tr className="ui-table-row-filter">
							<th className="px-4 py-2" />
							<th className="px-4 py-2">
								<span className="flex flex-wrap gap-1">
									{CAP_META.filter(
										(m) => existingCaps.has(m.key) || capFilter.has(m.key),
									).map((m) => {
										const isActive = capFilter.has(m.key);
										return (
											<button
												key={m.key}
												type="button"
												aria-pressed={isActive}
												onClick={() => toggleCapFilter(m.key)}
												className={`ui-badge inline-flex items-center px-1.5 py-0.5 text-[10px] font-medium border transition-colors ${
													isActive ? m.style : m.muted
												}`}
											>
												{m.label}
											</button>
										);
									})}
									{/* All output pills render unconditionally: filtering is
									    server-side, so a matching model may exist outside the
									    loaded window and the pill must stay reachable. */}
									{OUTPUT_META.map((m) => {
										const isActive = outputFilter.has(m.key);
										return (
											<button
												key={m.key}
												type="button"
												aria-pressed={isActive}
												onClick={() => toggleOutputFilter(m.key)}
												className={`ui-badge inline-flex items-center px-1.5 py-0.5 text-[10px] font-medium border transition-colors ${
													isActive ? m.style : m.muted
												}`}
											>
												{t(m.labelKey)}
											</button>
										);
									})}
									{(capFilter.size > 0 || outputFilter.size > 0) && (
										<button
											type="button"
											onClick={() => {
												setCapFilter(new Set());
												setOutputFilter(new Set());
											}}
											className="ui-badge inline-flex items-center px-1.5 py-0.5 text-[10px] font-medium text-gray-400 hover:text-gray-200"
										>
											✕
										</button>
									)}
								</span>
							</th>
							{showProviderCol && <th className="px-4 py-2" />}
							<th className="px-4 py-2" />
							<th aria-hidden />
							<th className="px-4 py-2" />
							<th aria-hidden />
							<th className="px-4 py-2" />
							<th aria-hidden />
							<th className="px-4 py-2" />
						</tr>
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
							virtualItems.map((vItem) => {
								const model = entries[vItem.index];
								const caps = parseCapabilities(model.capabilities);
								const isActive = model.enabled && !model.disabled_manually;
								const isManuallyDisabled =
									model.enabled && model.disabled_manually;
								return (
									<tr
										key={model.id}
										data-index={vItem.index}
										ref={virtualizer.measureElement}
										className={`hover:bg-(--surface-hover) ${vItem.index % 2 === 1 ? "ui-row-even" : ""} ${onModelClick ? "cursor-pointer" : ""}`}
										onClick={() => onModelClick?.(model)}
									>
										<td className="px-4 py-1.5">
											<div className="flex flex-col">
												<span
													className={`text-left text-sm ${isActive ? "font-medium text-white" : "text-gray-500"}`}
												>
													{model.name ||
														proxyModelID(model.provider_name, model.model_id)}
												</span>
												<CopyablePill
													text={proxyModelID(
														model.provider_name,
														model.model_id,
													)}
													textClassName="text-[11px] model-id-text font-mono leading-tight"
													tooltip={t("components.modelTable.clickToCopyId")}
												/>
											</div>
										</td>
										<td className="px-4 py-1.5">
											<div className="flex flex-wrap gap-1">
												{CAP_META.filter((m) => hasCap(caps, m.key)).map(
													(m) => (
														<span
															key={m.key}
															className={`ui-badge inline-flex items-center px-1.5 py-0.5 text-[10px] font-medium border ${m.style}`}
														>
															{m.label}
														</span>
													),
												)}
												<OutputBadges
													outputModalities={model.output_modalities}
												/>
											</div>
										</td>
										{showProviderCol && (
											<td
												className="px-4 py-1.5 whitespace-nowrap text-sm text-gray-300 truncate"
												title={model.provider_name}
											>
												{model.provider_name}
											</td>
										)}
										<td className="px-4 py-1.5 whitespace-nowrap text-sm text-gray-400">
											{formatRelativeTime(model.last_seen_at)}
										</td>
										<td aria-hidden />
										<td className="px-4 py-1.5 whitespace-nowrap text-sm text-gray-300">
											{formatNumber(model.context_length)}
										</td>
										<td aria-hidden />
										<td className="px-4 py-1.5 whitespace-nowrap text-sm text-gray-300">
											{formatNumber(model.max_output_tokens)}
										</td>
										<td aria-hidden />
										<td className="px-4 py-1.5 whitespace-nowrap">
											<span
												className={`ui-badge px-2 py-px leading-[1.6] text-xs ${
													isActive
														? "ui-badge-success"
														: isManuallyDisabled
															? "ui-badge-warning"
															: "ui-badge-error"
												}`}
												{...(!model.enabled && !model.disabled_manually
													? {
															title: t("models.disabledByDiscovery", {
																date: formatDate(model.last_seen_at),
															}),
															"data-testid": "disabled-by-discovery",
														}
													: {})}
											>
												<span className="badge-text">
													{isActive
														? t("common.enabled")
														: isManuallyDisabled
															? t("common.manuallyDisabled")
															: t("common.disabled")}
												</span>
											</span>
										</td>
									</tr>
								);
							})
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
			{confirmDeleteDisabled && onDeleteDisabled && (
				<ConfirmDialog
					title={t("components.virtualModelTable.deleteDisabledModels")}
					message={t("components.virtualModelTable.deleteDisabledMessage", {
						count: disabledCount,
					})}
					fields={disabledModels.map((m) =>
						proxyModelID(m.provider_name, m.model_id),
					)}
					confirmLabel={t("common.delete")}
					onConfirm={() => {
						onDeleteDisabled?.(disabledModelIds);
						setConfirmDeleteDisabled(false);
					}}
					onCancel={() => setConfirmDeleteDisabled(false)}
				/>
			)}
		</div>
	);
}
