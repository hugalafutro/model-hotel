import { Dices, Settings } from "lucide-react";
import { useMemo, useState } from "react";
import type { GenerationParams } from "../api/types";
import { FilterInput } from "./FilterInput";

interface ProviderInfo {
	name: string;
	base_url: string;
}

interface ModelItem {
	provider_name: string;
	model_id: string;
	display_name?: string;
}

interface SingleProps {
	multi?: false;
	models: ModelItem[];
	selected: string;
	onChange: (selected: string) => void;
	maxSelections?: number;
	label?: string;
	providers?: ProviderInfo[];
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
	models: ModelItem[];
	selected: string[];
	onChange: (selected: string[]) => void;
	maxSelections?: number;
	label?: string;
	providers?: ProviderInfo[];
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

type SortMode = "provider" | "model";

function getProviderStyle(baseUrl: string, active: boolean) {
	const isNanoGPT = baseUrl.includes("nano-gpt.com");
	const isDeepSeek = baseUrl.includes("deepseek.com");
	const isOllama = baseUrl.includes("ollama.com");
	const isZAI = baseUrl.includes("z.ai");
	if (active) {
		if (isNanoGPT)
			return "bg-[#0690a8] text-white border-[#0690a8] shadow-[0_0_6px_1px_rgba(6,144,168,0.35)]";
		if (isDeepSeek)
			return "bg-[#36aaff] text-white border-[#36aaff] shadow-[0_0_6px_1px_rgba(54,170,255,0.35)]";
		if (isZAI)
			return "bg-[#18181b] text-white border-[#18181b] shadow-[0_0_6px_1px_rgba(255,255,255,0.2)]";
		if (isOllama)
			return "bg-[#71717a] text-white border-[#71717a] shadow-[0_0_6px_1px_rgba(113,113,122,0.35)]";
		return "bg-(--surface-elevated) text-(--text-primary) border-(--border-input) shadow-[0_0_6px_1px_rgba(255,255,255,0.15)]";
	}
	if (isNanoGPT)
		return "bg-[#0690a8]/20 text-[#0690a8] border-[#0690a8]/50 hover:bg-[#0690a8]/30";
	if (isDeepSeek)
		return "bg-[#36aaff]/20 text-[#36aaff] border-[#36aaff]/50 hover:bg-[#36aaff]/30";
	if (isZAI)
		return "bg-[#18181b]/25 text-[#d4d4d8] border-[#3f3f46]/60 hover:bg-[#18181b]/40";
	if (isOllama)
		return "bg-[#71717a]/20 text-[#a1a1aa] border-[#71717a]/40 hover:bg-[#71717a]/30";
	return "bg-(--surface-hover) text-(--text-secondary) border-(--border-default) hover:bg-(--surface-elevated-hover)";
}

export function ModelPicker({
	models,
	selected,
	onChange,
	multi = false,
	maxSelections = Infinity,
	label,
	providers = [],
	align,
	exclude = [],
	slotParams,
	onConfigureParams,
	paramsReadonly = false,
	disabled = false,
	onRandom,
}: ModelPickerProps) {
	const [search, setSearch] = useState("");
	const [providerFilter, setProviderFilter] = useState<Set<string>>(new Set());
	const [sortMode, setSortMode] = useState<SortMode>("model");

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

	const providerBaseUrl = useMemo(() => {
		const map = new Map<string, string>();
		for (const p of providers) {
			map.set(p.name, p.base_url);
		}
		return map;
	}, [providers]);

	const filteredModels = useMemo(() => {
		let result = enabledModels;
		if (providerFilter.size > 0) {
			result = result.filter((m) => providerFilter.has(m.provider_name));
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
		result = [...result].sort((a, b) => {
			const aVal = proxyModelID(a.provider_name, a.model_id);
			const bVal = proxyModelID(b.provider_name, b.model_id);
			const aSel = selectedSet.has(aVal) ? 0 : 1;
			const bSel = selectedSet.has(bVal) ? 0 : 1;
			if (aSel !== bSel) return aSel - bSel;
			if (sortMode === "model") {
				const aName = (a.display_name || a.model_id).toLowerCase();
				const bName = (b.display_name || b.model_id).toLowerCase();
				const cmp = aName.localeCompare(bName);
				if (cmp !== 0) return cmp;
				return a.provider_name.localeCompare(b.provider_name);
			}
			const cmp = a.provider_name.localeCompare(b.provider_name);
			if (cmp !== 0) return cmp;
			return (a.display_name || a.model_id).localeCompare(
				b.display_name || b.model_id,
			);
		});
		return result;
	}, [enabledModels, providerFilter, search, selectedSet, sortMode]);

	const toggleProvider = (provider: string) => {
		setProviderFilter((prev) => {
			const next = new Set(prev);
			if (next.has(provider)) next.delete(provider);
			else next.add(provider);
			return next;
		});
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
				<label className="text-sm text-(--text-secondary) block">{label}</label>
			)}

			<div className="flex items-center gap-2 flex-wrap">
				<FilterInput
					value={search}
					onChange={setSearch}
					placeholder="Filter models…"
					className="w-[320px]"
					disabled={disabled}
				/>
				<div className="flex items-center gap-2 text-xs">
					<button
						onClick={() => setSortMode("model")}
						className={`cursor-pointer transition-colors ${sortMode === "model" ? "text-(--text-primary)" : "text-(--text-secondary) hover:text-(--text-primary)"}`}
					>
						Name
					</button>
					<div className="flex items-center gap-1.5 px-2 py-0.5 rounded-full border border-(--border-subtle) bg-(--surface-hover)">
						<button
							onClick={() => setSortMode("model")}
							className="flex items-center cursor-pointer"
						>
							<span
								className={`w-2 h-2 rounded-full transition-colors ${sortMode === "model" ? "bg-(--accent)" : "border border-(--border-subtle)"}`}
							/>
						</button>
						<span className="text-(--text-secondary)">Sort by</span>
						<button
							onClick={() => setSortMode("provider")}
							className="flex items-center cursor-pointer"
						>
							<span
								className={`w-2 h-2 rounded-full transition-colors ${sortMode === "provider" ? "bg-(--accent)" : "border border-(--border-subtle)"}`}
							/>
						</button>
					</div>
					<button
						onClick={() => setSortMode("provider")}
						className={`cursor-pointer transition-colors ${sortMode === "provider" ? "text-(--text-primary)" : "text-(--text-secondary) hover:text-(--text-primary)"}`}
					>
						Provider
					</button>
				</div>
				<div className="flex flex-wrap gap-1">
					{providerNames.map((name) => {
						const active = providerFilter.has(name);
						const baseUrl = providerBaseUrl.get(name) || "";
						return (
							<button
								key={name}
								onClick={() => toggleProvider(name)}
								className={`inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium border transition-colors ${getProviderStyle(baseUrl, active)}`}
							>
								{name}
							</button>
						);
					})}
					{providerFilter.size > 0 && (
						<button
							onClick={() => setProviderFilter(new Set())}
							className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium text-gray-400 hover:text-gray-200"
						>
							✕
						</button>
					)}
				</div>
			</div>

			<div
				className={`flex flex-wrap gap-1.5 h-40 overflow-y-auto pr-1 ${align === "right" ? "justify-end" : "justify-start"} ${disabled ? "opacity-50 pointer-events-none" : ""}`}
			>
				{onRandom && (
					<button
						type="button"
						onClick={onRandom}
						title="Random"
						className="cursor-pointer text-white/70 hover:text-(--accent) transition-colors p-1 -m-1 flex items-center self-center"
					>
						<Dices size={13} />
					</button>
				)}
				{filteredModels.map((m) => {
					const val = proxyModelID(m.provider_name, m.model_id);
					const isSelected = selectedSet.has(val);
					const hasParams = !!(
						slotParams?.[val] &&
						Object.values(slotParams[val]).some((v) => v !== undefined)
					);
					return (
						<div
							key={val}
							className={`inline-flex items-center gap-1 px-2 py-0.5 text-[11px] rounded-md border transition-all whitespace-nowrap ${
								isSelected
									? "bg-(--accent)/15 border-(--accent)/40 text-(--accent)"
									: "bg-(--surface-hover) border-(--border-subtle) text-(--text-secondary) hover:text-(--text-primary)"
							}`}
							title={`${m.provider_name}/${m.display_name || m.model_id}`}
						>
							<button
								onClick={() => toggleModel(val)}
								className={`${disabled ? "cursor-not-allowed" : "cursor-pointer"}`}
								disabled={disabled}
							>
								{m.display_name || m.model_id}
							</button>
							{isSelected && onConfigureParams && (
								<button
									onClick={(e) => {
										e.stopPropagation();
										onConfigureParams(val);
									}}
									disabled={paramsReadonly}
									className={`shrink-0 flex items-center transition-all ${
										paramsReadonly
											? "opacity-30 cursor-not-allowed"
											: "cursor-pointer hover:drop-shadow-[0_0_6px_var(--accent)] hover:text-(--accent)"
									}`}
									title={
										paramsReadonly
											? "Parameters locked while running"
											: hasParams
												? "Edit generation parameters"
												: "Add generation parameters"
									}
								>
									<Settings
										size={10}
										className={hasParams ? "text-(--accent)" : "text-white"}
									/>
								</button>
							)}
						</div>
					);
				})}
				{filteredModels.length === 0 && (
					<span className="text-xs text-(--text-muted)">No models match</span>
				)}
			</div>
		</div>
	);
}
