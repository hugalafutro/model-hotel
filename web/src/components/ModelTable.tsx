import { useCallback, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import type { Model, Provider } from "../api/types";
import { formatRelativeTime, formatTokens } from "../utils/format";
import { parseCapabilities, proxyModelID } from "../utils/model";
import { CapBadge } from "./CapBadge";
import { ConfirmDialog } from "./ConfirmDialog";
import { CopyablePill } from "./CopyablePill";
import { CAP_META, type CapKey, hasCap, matchesAllCaps } from "./capMeta";
import type { SortState } from "./DataTable";
import { EmptyRow, PaginationBar, Row, SortableHeader } from "./DataTable";
import { FilterInput } from "./FilterInput";
import {
	MODEL_COL_WIDTHS_NO_PROVIDER,
	MODEL_COL_WIDTHS_WITH_PROVIDER,
} from "./modelTableWidths";
import { ProviderFilter } from "./ProviderFilter";

export type SortField =
	| "name"
	| "capabilities"
	| "provider"
	| "discovered"
	| "context"
	| "output"
	| "status";

export interface ModelTableProps {
	models: Model[];
	providers?: Provider[];
	initialProviderFilter?: Set<string>;
	onModelClick?: (model: Model) => void;
	/** When provided, shows a "Delete disabled" button. Called with IDs of disabled models. */
	onDeleteDisabled?: (ids: string[]) => void;
}

export function ModelTable({
	models,
	providers,
	initialProviderFilter,
	onModelClick,
	onDeleteDisabled,
}: ModelTableProps) {
	const [searchQuery, setSearchQuery] = useState("");
	const [selectedProviders, setSelectedProviders] = useState<Set<string>>(
		initialProviderFilter ?? new Set(),
	);
	const [sort, setSort] = useState<SortState<SortField>>({
		field: "name",
		dir: "asc",
	});
	const { t } = useTranslation();
	const [capFilter, setCapFilter] = useState<Set<CapKey>>(new Set());
	const [confirmDeleteDisabled, setConfirmDeleteDisabled] = useState(false);

	const [pageSize, setPageSize] = useState(20);
	const [currentPage, setCurrentPage] = useState(1);

	const showProviderCol = providers !== undefined;

	const { sortedAndFiltered, pillAvailability, existingCaps } = useMemo(() => {
		if (!models) {
			return {
				sortedAndFiltered: [],
				pillAvailability: new Map<CapKey, boolean>(),
				existingCaps: new Set<CapKey>(),
			};
		}

		const baseFiltered = models.filter(
			(model) =>
				proxyModelID(model.provider_name, model.model_id)
					.toLowerCase()
					.includes(searchQuery.toLowerCase()) ||
				model.name?.toLowerCase().includes(searchQuery.toLowerCase()) ||
				model.display_name?.toLowerCase().includes(searchQuery.toLowerCase()),
		);

		const capsInData = new Set<CapKey>();
		for (const m of baseFiltered) {
			const c = parseCapabilities(m.capabilities);
			for (const meta of CAP_META) {
				if (hasCap(c, meta.key)) capsInData.add(meta.key);
			}
		}

		let filtered = baseFiltered;

		if (selectedProviders.size > 0) {
			filtered = filtered.filter((m) => selectedProviders.has(m.provider_id));
		}

		if (capFilter.size > 0) {
			filtered = filtered.filter((m) =>
				matchesAllCaps(parseCapabilities(m.capabilities), capFilter),
			);
		}

		const availability = new Map<CapKey, boolean>();
		for (const m of CAP_META) {
			const testFilter = new Set(capFilter);
			testFilter.add(m.key);
			const hasMatch = baseFiltered.some((model) =>
				matchesAllCaps(parseCapabilities(model.capabilities), testFilter),
			);
			availability.set(m.key, hasMatch);
		}

		const dir = sort.dir === "asc" ? 1 : -1;
		filtered.sort((a, b) => {
			switch (sort.field) {
				case "name":
					return (
						dir *
						(a.name || proxyModelID(a.provider_name, a.model_id)).localeCompare(
							b.name || proxyModelID(b.provider_name, b.model_id),
						)
					);
				case "provider":
					return dir * a.provider_name.localeCompare(b.provider_name);
				case "discovered":
					return (
						dir *
						(new Date(a.last_seen_at).getTime() -
							new Date(b.last_seen_at).getTime())
					);
				case "context":
					return dir * ((a.context_length ?? 0) - (b.context_length ?? 0));
				case "output":
					return (
						dir * ((a.max_output_tokens ?? 0) - (b.max_output_tokens ?? 0))
					);
				case "capabilities": {
					const capsA = Object.values(parseCapabilities(a.capabilities)).filter(
						Boolean,
					).length;
					const capsB = Object.values(parseCapabilities(b.capabilities)).filter(
						Boolean,
					).length;
					return dir * (capsA - capsB);
				}
				case "status":
					return dir * (a.enabled === b.enabled ? 0 : a.enabled ? -1 : 1);
				default:
					return 0;
			}
		});

		return {
			sortedAndFiltered: filtered,
			pillAvailability: availability,
			existingCaps: capsInData,
		};
	}, [models, searchQuery, sort, capFilter, selectedProviders]);

	const disabledModelIds = useMemo(
		() => sortedAndFiltered.filter((m) => !m.enabled).map((m) => m.id),
		[sortedAndFiltered],
	);
	const disabledCount = disabledModelIds.length;

	const toggleCapFilter = useCallback((key: CapKey) => {
		setCapFilter((prev) => {
			const next = new Set(prev);
			if (next.has(key)) next.delete(key);
			else next.add(key);
			return next;
		});
		setCurrentPage(1);
	}, []);

	const handleSort = (field: SortField) => {
		setSort((prev) => ({
			field,
			dir: prev.field === field && prev.dir === "asc" ? "desc" : "asc",
		}));
		setCurrentPage(1);
	};

	const totalPages = Math.ceil(sortedAndFiltered.length / pageSize);
	const paginatedModels = sortedAndFiltered.slice(
		(currentPage - 1) * pageSize,
		currentPage * pageSize,
	);

	const colSpan = showProviderCol ? 10 : 9;

	return (
		<div className="space-y-4">
			<div className="flex items-center gap-4">
				<div className="flex items-center gap-2 shrink-0">
					<FilterInput
						value={searchQuery}
						onChange={(v) => {
							setSearchQuery(v);
							setCurrentPage(1);
						}}
						placeholder={t("components.modelTable.searchModels")}
						className="w-[320px]"
						autoFocus
					/>
					{onDeleteDisabled && disabledCount > 0 && (
						<button
							type="button"
							onClick={() => setConfirmDeleteDisabled(true)}
							className="flex items-center gap-1 px-2 py-1 rounded text-xs font-medium text-red-400 border border-red-500/30 bg-red-500/10 hover:bg-red-500/20 hover:border-red-400/50 transition-colors cursor-pointer"
							aria-label={t("components.modelTable.deleteDisabledAria", {
								count: disabledCount,
							})}
						>
							{t("components.modelTable.deleteDisabled", {
								count: disabledCount,
							})}
						</button>
					)}
				</div>
				<div className="flex-1 flex justify-end">
					{models && models.length > 0 && (
						<PaginationBar
							page={currentPage}
							totalPages={totalPages}
							totalItems={sortedAndFiltered.length}
							pageSize={pageSize}
							onPageChange={setCurrentPage}
							onPageSizeChange={(s) => {
								setPageSize(s);
								setCurrentPage(1);
							}}
							label="models"
						/>
					)}
					<div className="flex-1 flex justify-end">
						{models && models.length > 0 && (
							<PaginationBar
								page={currentPage}
								totalPages={totalPages}
								totalItems={sortedAndFiltered.length}
								pageSize={pageSize}
								onPageChange={setCurrentPage}
								onPageSizeChange={(s) => {
									setPageSize(s);
									setCurrentPage(1);
								}}
								label={t("components.modelTable.models")}
							/>
						)}
					</div>
				</div>
			</div>

			<div className="ui-card overflow-hidden">
				<table className="min-w-full table-fixed ui-table">
					<colgroup>
						{(showProviderCol
							? MODEL_COL_WIDTHS_WITH_PROVIDER
							: MODEL_COL_WIDTHS_NO_PROVIDER
						).map((w, i) => (
							// biome-ignore lint/suspicious/noArrayIndexKey: static col widths array, order never changes
							<col key={i} className={w} />
						))}
					</colgroup>
					<thead>
						<tr>
							<SortableHeader
								label={t("components.modelTable.model")}
								field="name"
								sort={sort}
								onSort={handleSort}
								tooltip={t("components.modelTable.modelNameAndId")}
							/>
							<th className="px-4 py-2 text-left text-xs font-medium uppercase tracking-wider whitespace-nowrap ui-table-header-text">
								{t("components.modelDetailPanel.capabilities")}
							</th>
							{showProviderCol && (
								<SortableHeader
									label={t("components.modelTable.provider")}
									field="provider"
									sort={sort}
									onSort={handleSort}
									tooltip={t("components.modelTable.providerName")}
								/>
							)}
							<SortableHeader
								label={t("components.modelTable.discovered")}
								field="discovered"
								sort={sort}
								onSort={handleSort}
								tooltip={t("components.modelTable.whenModelDiscovered")}
							/>
							<th aria-hidden />
							<SortableHeader
								label={t("components.modelTable.context")}
								field="context"
								sort={sort}
								onSort={handleSort}
								tooltip={t("components.modelTable.maximumContextLength")}
							/>
							<th aria-hidden />
							<SortableHeader
								label={t("components.modelTable.maxOutput")}
								field="output"
								sort={sort}
								onSort={handleSort}
								tooltip={t("components.modelTable.maximumOutputTokens")}
							/>
							<th aria-hidden />
							<SortableHeader
								label={t("components.modelTable.status")}
								field="status"
								sort={sort}
								onSort={handleSort}
								tooltip={t("components.modelTable.modelStatus")}
							/>
						</tr>
						<tr className="ui-table-row-filter">
							<th className="px-4 py-2" />
							<th className="px-4 py-2">
								<span className="flex flex-wrap gap-1">
									{CAP_META.filter((m) => existingCaps.has(m.key)).map((m) => {
										const isActive = capFilter.has(m.key);
										const isAvailable = pillAvailability.get(m.key) ?? false;
										const isDisabled = !isActive && !isAvailable;
										return (
											<button
												key={m.key}
												type="button"
												disabled={isDisabled}
												onClick={() => toggleCapFilter(m.key)}
												className={`inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium border transition-colors ${isActive ? m.style : isDisabled ? m.disabled : m.muted}`}
											>
												{m.label}
											</button>
										);
									})}
									{capFilter.size > 0 && (
										<button
											type="button"
											onClick={() => {
												setCapFilter(new Set());
												setCurrentPage(1);
											}}
											className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium text-gray-400 hover:text-gray-200"
										>
											✕
										</button>
									)}
								</span>
							</th>
							{showProviderCol && (
								<th className="px-4 py-2">
									<ProviderFilter
										providers={providers}
										selected={selectedProviders}
										onChange={(next) => {
											setSelectedProviders(next);
											setCurrentPage(1);
										}}
									/>
								</th>
							)}
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
						{paginatedModels.length > 0 ? (
							paginatedModels.map((model) => {
								const caps = parseCapabilities(model.capabilities);
								return (
									<Row key={model.id} onClick={() => onModelClick?.(model)}>
										<td className="px-4 py-1.5">
											<div
												className={`flex flex-col ${model.enabled ? "" : "opacity-50"}`}
											>
												<span className="text-left text-sm font-medium text-white">
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
												{CAP_META.map((m) => (
													<CapBadge key={m.key} caps={caps} capKey={m.key} />
												))}
											</div>
										</td>
										{showProviderCol && (
											<td className="px-4 py-1.5 whitespace-nowrap text-sm text-gray-300">
												{model.provider_name}
											</td>
										)}
										<td className="px-4 py-1.5 whitespace-nowrap text-sm text-gray-400">
											{formatRelativeTime(model.last_seen_at)}
										</td>
										<td aria-hidden />
										<td className="px-4 py-1.5 whitespace-nowrap text-sm text-gray-300">
											{formatTokens(model.context_length)}
										</td>
										<td aria-hidden />
										<td className="px-4 py-1.5 whitespace-nowrap text-sm text-gray-300">
											{formatTokens(model.max_output_tokens)}
										</td>
										<td aria-hidden />
										<td className="px-4 py-1.5 whitespace-nowrap">
											<span
												className={`px-2 py-0.5 text-xs rounded-full ${model.enabled && !model.disabled_manually ? "bg-green-900/50 text-green-400" : model.enabled && model.disabled_manually ? "bg-yellow-900/50 text-yellow-400" : "bg-red-900/50 text-red-400"}`}
											>
												{model.enabled && !model.disabled_manually
													? t("common.enabled")
													: model.enabled && model.disabled_manually
														? t("common.manuallyDisabled")
														: t("common.disabled")}
											</span>
										</td>
									</Row>
								);
							})
						) : (
							<EmptyRow
								colSpan={colSpan}
								message={
									searchQuery ||
									selectedProviders.size > 0 ||
									capFilter.size > 0
										? t("components.modelTable.noModelsMatchFilters")
										: t("components.modelTable.noModelsDiscovered")
								}
							/>
						)}
					</tbody>
				</table>
			</div>
			{confirmDeleteDisabled && onDeleteDisabled && (
				<ConfirmDialog
					title={t("components.modelTable.deleteDisabledModels")}
					message={t("components.modelTable.deleteDisabledMessage", {
						count: disabledCount,
					})}
					fields={[
						t("components.modelTable.deleteDisabledLabel", {
							count: disabledCount,
						}),
					]}
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
