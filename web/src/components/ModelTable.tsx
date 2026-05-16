import { X } from "lucide-react";
import { useCallback, useMemo, useState } from "react";
import type { Model, Provider } from "../api/types";
import { formatRelativeTime, formatTokens } from "../utils/format";
import { parseCapabilities, proxyModelID } from "../utils/model";
import { CapBadge } from "./CapBadge";
import { CopyablePill } from "./CopyablePill";
import { CAP_META, type CapKey, hasCap, matchesAllCaps } from "./capMeta";
import type { SortState } from "./DataTable";
import { EmptyRow, PaginationBar, Row, SortableHeader } from "./DataTable";
import { FilterInput } from "./FilterInput";
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
}

export function ModelTable({
	models,
	providers,
	initialProviderFilter,
	onModelClick,
}: ModelTableProps) {
	const [searchQuery, setSearchQuery] = useState("");
	const [selectedProviders, setSelectedProviders] = useState<Set<string>>(
		initialProviderFilter ?? new Set(),
	);
	const [sort, setSort] = useState<SortState<SortField>>({
		field: "name",
		dir: "asc",
	});
	const [capFilter, setCapFilter] = useState<Set<CapKey>>(new Set());
	const [statusFilter, setStatusFilter] = useState<Set<"enabled" | "disabled">>(
		new Set(),
	);
	const showAllStatus = statusFilter.size === 0 || statusFilter.size === 2;
	const [pageSize, setPageSize] = useState(20);
	const [currentPage, setCurrentPage] = useState(1);

	const showProviderCol = providers !== undefined;

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
	};

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

		if (!showAllStatus) {
			filtered = filtered.filter((m) =>
				statusFilter.has("enabled") ? m.enabled : !m.enabled,
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
	}, [
		models,
		searchQuery,
		sort,
		capFilter,
		selectedProviders,
		statusFilter,
		showAllStatus,
	]);

	const totalPages = Math.ceil(sortedAndFiltered.length / pageSize);
	const paginatedModels = sortedAndFiltered.slice(
		(currentPage - 1) * pageSize,
		currentPage * pageSize,
	);

	const colSpan = showProviderCol ? 7 : 6;

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
						placeholder="Search models…"
						className="w-[320px]"
						autoFocus
					/>
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
				</div>
			</div>

			<div className="ui-card">
				<table className="min-w-full table-fixed ui-table">
					<colgroup>
						{showProviderCol ? (
							<>
								<col className="w-[32%]" />
								<col className="w-[26%]" />
								<col className="w-[17%]" />
								<col className="w-[6%]" />
								<col className="w-[4%]" />
								<col className="w-[4%]" />
								<col className="w-[11%]" />
							</>
						) : (
							<>
								<col className="w-[40%]" />
								<col className="w-[30%]" />
								<col className="w-[10%]" />
								<col className="w-[6%]" />
								<col className="w-[6%]" />
								<col className="w-[8%]" />
							</>
						)}
					</colgroup>
					<thead>
						<tr>
							<SortableHeader
								label="Model"
								field="name"
								sort={sort}
								onSort={handleSort}
								tooltip="Model name and ID"
							/>
							<th className="px-4 py-2 text-left text-xs font-medium uppercase tracking-wider whitespace-nowrap ui-table-header-text select-none hover:text-gray-200">
								Capabilities
							</th>
							{showProviderCol && (
								<th className="px-4 py-2 text-left text-xs font-medium uppercase tracking-wider ui-table-header-text select-none hover:text-gray-200">
									Provider
								</th>
							)}
							<SortableHeader
								label="Discovered"
								field="discovered"
								sort={sort}
								onSort={handleSort}
								tooltip="When the model was last seen/discovered"
							/>
							<SortableHeader
								label="Ctx"
								field="context"
								sort={sort}
								onSort={handleSort}
								tooltip="Maximum context length in tokens"
							/>
							<SortableHeader
								label="Max Out"
								field="output"
								sort={sort}
								onSort={handleSort}
								tooltip="Maximum output tokens"
							/>
							<th className="px-4 py-2 text-left text-xs font-medium uppercase tracking-wider ui-table-header-text select-none hover:text-gray-200">
								Status
							</th>
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
							<th className="px-4 py-2" />
							<th className="px-4 py-2" />
							<th className="px-4 py-2">
								<span className="flex flex-wrap items-center gap-1">
									<button
										type="button"
										onClick={() => {
											setStatusFilter((prev) => {
												const next = new Set(prev);
												if (next.has("enabled")) {
													next.delete("enabled");
												} else {
													next.add("enabled");
												}
												return next;
											});
											setCurrentPage(1);
										}}
										className={`inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium border transition-colors ${
											statusFilter.has("enabled")
												? "bg-green-900/40 text-green-300 border-green-700/50 shadow-[0_0_6px_1px_rgba(34,197,94,0.35)]"
												: "bg-green-900/15 text-green-500/60 border-green-700/25 hover:bg-green-900/25 hover:text-green-400"
										}`}
									>
										Enabled
									</button>
									<button
										type="button"
										onClick={() => {
											setStatusFilter((prev) => {
												const next = new Set(prev);
												if (next.has("disabled")) {
													next.delete("disabled");
												} else {
													next.add("disabled");
												}
												return next;
											});
											setCurrentPage(1);
										}}
										className={`inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium border transition-colors ${
											statusFilter.has("disabled")
												? "bg-red-900/40 text-red-300 border-red-700/50 shadow-[0_0_6px_1px_rgba(239,68,68,0.35)]"
												: "bg-red-900/15 text-red-500/60 border-red-700/25 hover:bg-red-900/25 hover:text-red-400"
										}`}
									>
										Disabled
									</button>
									{statusFilter.size === 2 && (
										<button
											type="button"
											onClick={() => {
												setStatusFilter(new Set());
												setCurrentPage(1);
											}}
											className="inline-flex items-center justify-center w-4 h-4 rounded text-gray-400 hover:text-gray-200 transition-colors"
										>
											<X size={10} />
										</button>
									)}
								</span>
							</th>
						</tr>
					</thead>
					<tbody>
						{paginatedModels.length > 0 ? (
							paginatedModels.map((model) => {
								const caps = parseCapabilities(model.capabilities);
								return (
									<Row key={model.id} onClick={() => onModelClick?.(model)}>
										<td className="px-4 py-1.5">
											<div className="flex flex-col">
												<span className="text-left text-sm font-medium text-white">
													{model.name ||
														proxyModelID(model.provider_name, model.model_id)}
												</span>
												<CopyablePill
													text={proxyModelID(
														model.provider_name,
														model.model_id,
													)}
													textClassName="text-[11px] text-gray-500 font-mono leading-tight"
													tooltip="Click to copy model ID"
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
										<td className="px-4 py-1.5 whitespace-nowrap text-sm text-gray-300">
											{formatTokens(model.context_length)}
										</td>
										<td className="px-4 py-1.5 whitespace-nowrap text-sm text-gray-300">
											{formatTokens(model.max_output_tokens)}
										</td>
										<td className="px-4 py-1.5 whitespace-nowrap">
											<span
												className={`px-2 py-0.5 text-xs rounded-full ${model.enabled ? "bg-green-900/50 text-green-400" : "bg-red-900/50 text-red-400"}`}
											>
												{model.enabled ? "Enabled" : "Disabled"}
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
										? "No models match your filters"
										: "No models discovered yet. Add a provider and discover models."
								}
							/>
						)}
					</tbody>
				</table>
			</div>
		</div>
	);
}
