import {
	ChevronDown,
	ChevronsDownUp,
	ChevronsUpDown,
	Dices,
	Settings,
} from "lucide-react";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import type { GenerationParams } from "../api/types";
import { parseCapabilities } from "../utils/model";
import type { CapKey } from "./capMeta";
import { CAP_META, hasCap, matchesAllCaps } from "./capMeta";
import { FilterInput } from "./FilterInput";
import { ProviderFilter } from "./ProviderFilter";

export interface ModelItem {
	provider_name: string;
	model_id: string;
	display_name?: string;
	capabilities?: string;
}

interface SingleProps {
	multi?: false;
	id?: string;
	models: ModelItem[];
	selected: string;
	onChange: (selected: string) => void;
	maxSelections?: number;
	label?: string;
	align?: "left" | "right";
	exclude?: string[];
	/** Per-model generation params shown on selected pills */
	slotParams?: Record<string, GenerationParams>;
	/** Called when user clicks the cog on a selected pill */
	onConfigureParams?: (modelId: string) => void;
	/** When true, param cogs are non-interactive (e.g. arena is running) */
	paramsReadonly?: boolean;
	/** When true, the picker is disabled (e.g. conversation is running) */
	disabled?: boolean;
	/** Called when random button is clicked */
	onRandom?: () => void;
}

interface MultiProps {
	multi: true;
	id?: string;
	models: ModelItem[];
	selected: string[];
	onChange: (selected: string[]) => void;
	maxSelections?: number;
	label?: string;
	align?: "left" | "right";
	exclude?: string[];
	/** Per-model generation params shown on selected pills */
	slotParams?: Record<string, GenerationParams>;
	/** Called when user clicks the cog on a selected pill */
	onConfigureParams?: (modelId: string) => void;
	/** When true, param cogs are non-interactive (e.g. arena is running) */
	paramsReadonly?: boolean;
	/** When true, the picker is disabled (e.g. conversation is running) */
	disabled?: boolean;
	/** Called when random button is clicked */
	onRandom?: () => void;
}

type ModelPickerProps = SingleProps | MultiProps;

function proxyModelID(providerName: string, modelId: string): string {
	return `${providerName.replace(/ /g, "-")}/${modelId}`;
}

export function ModelPicker({
	id,
	models,
	selected,
	onChange,
	multi = false,
	maxSelections = Infinity,
	label,
	align,
	exclude = [],
	slotParams,
	onConfigureParams,
	paramsReadonly = false,
	disabled = false,
	onRandom,
}: ModelPickerProps) {
	const { t } = useTranslation();
	const [search, setSearch] = useState("");
	const [providerFilter, setProviderFilter] = useState<Set<string>>(new Set());
	const [capFilter, setCapFilter] = useState<Set<CapKey>>(new Set());
	const [collapsedProviders, setCollapsedProviders] = useState<Set<string>>(
		new Set(),
	);

	const selectedSet = useMemo(() => {
		if (multi) return new Set(selected as string[]);
		return new Set(selected ? [selected as string] : []);
	}, [selected, multi]);

	const enabledModels = useMemo(() => {
		const excludeSet = new Set(exclude);
		return models.filter(
			(m) =>
				m.provider_name &&
				!excludeSet.has(proxyModelID(m.provider_name, m.model_id)),
		);
	}, [models, exclude]);

	const providerNames = useMemo(
		() => Array.from(new Set(enabledModels.map((m) => m.provider_name))).sort(),
		[enabledModels],
	);

	// Which capabilities exist in the current dataset (for showing/hiding pills)
	const existingCaps = useMemo(() => {
		const caps = new Set<CapKey>();
		for (const m of enabledModels) {
			if (!m.capabilities) continue;
			const parsed = parseCapabilities(m.capabilities);
			for (const meta of CAP_META) {
				if (hasCap(parsed, meta.key)) caps.add(meta.key);
			}
		}
		return caps;
	}, [enabledModels]);

	// Whether adding a cap to the filter would still yield results
	const capAvailability = useMemo(() => {
		const availability = new Map<CapKey, boolean>();
		for (const meta of CAP_META) {
			const testFilter = new Set(capFilter);
			testFilter.add(meta.key);
			const hasMatch = enabledModels.some((m) => {
				if (!m.capabilities) return false;
				return matchesAllCaps(parseCapabilities(m.capabilities), testFilter);
			});
			availability.set(meta.key, hasMatch);
		}
		return availability;
	}, [enabledModels, capFilter]);

	const filteredModels = useMemo(() => {
		let result = enabledModels;
		if (providerFilter.size > 0) {
			result = result.filter((m) => providerFilter.has(m.provider_name));
		}
		if (capFilter.size > 0) {
			result = result.filter((m) => {
				if (!m.capabilities) return false;
				return matchesAllCaps(parseCapabilities(m.capabilities), capFilter);
			});
		}
		if (search.trim()) {
			const q = search.trim().toLowerCase();
			result = result.filter((m) => {
				const name = (m.display_name || m.model_id).toLowerCase();
				const pid = m.model_id.toLowerCase();
				const prov = m.provider_name.toLowerCase();
				return name.includes(q) || pid.includes(q) || prov.includes(q);
			});
		}
		return [...result].sort((a, b) => {
			const aVal = proxyModelID(a.provider_name, a.model_id);
			const bVal = proxyModelID(b.provider_name, b.model_id);
			const aSel = selectedSet.has(aVal) ? 0 : 1;
			const bSel = selectedSet.has(bVal) ? 0 : 1;
			if (aSel !== bSel) return aSel - bSel;
			const cmp = a.provider_name.localeCompare(b.provider_name);
			if (cmp !== 0) return cmp;
			return (a.display_name || a.model_id).localeCompare(
				b.display_name || b.model_id,
			);
		});
	}, [enabledModels, providerFilter, capFilter, search, selectedSet]);

	const groupedModels = useMemo(() => {
		const groups = new Map<string, ModelItem[]>();
		for (const m of filteredModels) {
			const existing = groups.get(m.provider_name);
			if (existing) {
				existing.push(m);
			} else {
				groups.set(m.provider_name, [m]);
			}
		}
		return groups;
	}, [filteredModels]);

	const toggleCollapse = (provider: string) => {
		setCollapsedProviders((prev) => {
			const next = new Set(prev);
			if (next.has(provider)) next.delete(provider);
			else next.add(provider);
			return next;
		});
	};

	const collapseAll = () => {
		setCollapsedProviders(new Set([...groupedModels.keys()]));
	};

	const expandAll = () => {
		setCollapsedProviders(new Set());
	};

	const toggleModel = (val: string) => {
		if (disabled) return;
		if (multi) {
			const current = [...(selected as string[])];
			if (current.includes(val)) {
				(onChange as (s: string[]) => void)(current.filter((v) => v !== val));
			} else {
				if (current.length >= maxSelections) return;
				(onChange as (s: string[]) => void)([...current, val]);
			}
		} else {
			(onChange as (s: string) => void)(val === selected ? "" : val);
		}
	};

	return (
		<div className="space-y-3">
			{label && (
				<label
					htmlFor={id ?? "model-picker-filter"}
					className="text-sm font-semibold text-(--accent) block"
				>
					{label}
				</label>
			)}

			<div className="flex items-center gap-2">
				<FilterInput
					id={id ?? "model-picker-filter"}
					value={search}
					onChange={setSearch}
					placeholder={t("components.modelPicker.filterModels")}
					className="w-[320px]"
					disabled={disabled}
				/>
				<div className="w-48 shrink-0">
					<ProviderFilter
						providers={providerNames.map((name) => ({ id: name, name }))}
						selected={providerFilter}
						onChange={setProviderFilter}
					/>
				</div>
			</div>

			{existingCaps.size > 0 && (
				<div className="flex flex-wrap gap-1 py-0.5">
					{CAP_META.filter((m) => existingCaps.has(m.key)).map((m) => {
						const isActive = capFilter.has(m.key);
						const isAvailable = capAvailability.get(m.key) ?? false;
						const isDisabled = !isActive && !isAvailable;
						return (
							<button
								key={m.key}
								type="button"
								disabled={isDisabled}
								onClick={() => {
									setCapFilter((prev) => {
										const next = new Set(prev);
										if (next.has(m.key)) next.delete(m.key);
										else next.add(m.key);
										return next;
									});
								}}
								className={`ui-tab inline-flex items-center px-1.5 py-0.5 text-[10px] font-medium border transition-colors ${
									isActive ? m.style : isDisabled ? m.disabled : m.muted
								}`}
							>
								{m.label}
							</button>
						);
					})}
					{capFilter.size > 0 && (
						<button
							type="button"
							onClick={() => setCapFilter(new Set())}
							className="ui-tab inline-flex items-center px-1.5 py-0.5 text-[10px] font-medium text-gray-400 hover:text-gray-200"
							aria-label={t("components.modelPicker.clearFilter")}
						>
							✕
						</button>
					)}
				</div>
			)}

			<div className="flex gap-1">
				{(onRandom || groupedModels.size > 0) && (
					<div className="flex flex-col items-center gap-1 pt-0.5 shrink-0">
						{onRandom && (
							<button
								type="button"
								onClick={onRandom}
								title={t("common.random")}
								aria-label={t("common.random")}
								className="ui-icon-btn p-1 flex items-center"
							>
								<Dices size={13} />
							</button>
						)}
						<button
							type="button"
							onClick={
								collapsedProviders.size === groupedModels.size
									? expandAll
									: collapseAll
							}
							title={
								collapsedProviders.size === groupedModels.size
									? t("components.modelPicker.expandAllProviders")
									: t("components.modelPicker.collapseAllProviders")
							}
							className="ui-icon-btn p-1 flex items-center"
						>
							{collapsedProviders.size === groupedModels.size ? (
								<ChevronsUpDown size={13} />
							) : (
								<ChevronsDownUp size={13} />
							)}
						</button>
					</div>
				)}
				<div
					className={`h-40 overflow-y-auto pr-1 flex-1 min-w-0 ${disabled ? "opacity-50 pointer-events-none" : ""}`}
				>
					{[...groupedModels].map(([providerName, providerModels]) => {
						const isCollapsed = collapsedProviders.has(providerName);
						return (
							<div key={providerName} className="mt-px">
								<button
									type="button"
									onClick={() => toggleCollapse(providerName)}
									className={`flex items-center gap-1.5 w-full text-[10px] font-medium transition-colors text-(--text-secondary) hover:text-(--text-primary)`}
								>
									<ChevronDown
										size={10}
										className={`transition-transform duration-200 ${isCollapsed ? "-rotate-90" : ""}`}
									/>
									<span>{providerName}</span>
									<span className="text-(--text-muted) font-normal">
										({providerModels.length})
									</span>
								</button>
								<div
									className={`grid transition-[grid-template-rows] duration-200 ease-in-out ${isCollapsed ? "grid-rows-[0fr]" : "grid-rows-[1fr]"}`}
								>
									<div
										className={`flex flex-wrap gap-0.5 overflow-hidden ${align === "right" ? "justify-end" : "justify-start"}`}
									>
										{providerModels.map((m) => {
											const val = proxyModelID(m.provider_name, m.model_id);
											const isSelected = selectedSet.has(val);
											const hasParams = !!(
												slotParams?.[val] &&
												Object.values(slotParams[val]).some(
													(v) => v !== undefined,
												)
											);
											return (
												<div
													key={val}
													className={`ui-tab inline-flex items-center gap-1 px-2 py-0.5 text-[11px] border transition-all whitespace-nowrap ${
														isSelected
															? "bg-(--accent)/15 border-(--accent)/40 text-(--accent)"
															: "bg-(--surface-hover) border-(--border-subtle) text-(--text-secondary) hover:text-(--text-primary)"
													}`}
													title={`${m.provider_name}/${m.display_name || m.model_id}`}
												>
													<button
														type="button"
														onClick={() => toggleModel(val)}
														className={`${disabled ? "cursor-not-allowed" : ""}`}
														disabled={disabled}
													>
														{m.display_name || m.model_id}
													</button>
													{isSelected && onConfigureParams && (
														<button
															type="button"
															onClick={(e) => {
																e.stopPropagation();
																onConfigureParams(val);
															}}
															disabled={paramsReadonly}
															className="ui-icon-btn shrink-0 flex items-center"
															title={
																paramsReadonly
																	? t("components.modelPicker.paramsLocked")
																	: hasParams
																		? t("components.modelPicker.editParams")
																		: t("components.modelPicker.addParams")
															}
														>
															<Settings
																size={10}
																className={
																	hasParams ? "text-(--accent)" : "text-white"
																}
															/>
														</button>
													)}
												</div>
											);
										})}
									</div>
								</div>
							</div>
						);
					})}
					{filteredModels.length === 0 && (
						<span className="text-xs text-(--text-muted)">
							{t("components.modelPicker.noModelsMatch")}
						</span>
					)}
				</div>
			</div>
		</div>
	);
}
