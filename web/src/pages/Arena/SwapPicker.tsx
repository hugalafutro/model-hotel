import { ChevronDown, ChevronsDownUp, ChevronsUpDown } from "lucide-react";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { FilterInput } from "../../components/FilterInput";
import { proxyModelID } from "../../utils/model";
import type { SwapPickerProps } from "./types";

export function SwapPicker({
	enabledModels,
	disabledModels,
	alreadyUsed,
	onSelect,
}: SwapPickerProps) {
	const { t } = useTranslation();
	const [search, setSearch] = useState("");
	const [collapsedProviders, setCollapsedProviders] = useState<Set<string>>(
		new Set(),
	);

	const available = useMemo(() => {
		const usedSet = new Set(alreadyUsed);
		return enabledModels.filter((m) => {
			const id = proxyModelID(m.provider_name, m.model_id);
			if (disabledModels.has(id)) return false;
			if (usedSet.has(id)) return false;
			if (search.trim()) {
				const q = search.trim().toLowerCase();
				const name = (m.display_name || m.model_id).toLowerCase();
				return name.includes(q) || m.model_id.toLowerCase().includes(q);
			}
			return true;
		});
	}, [enabledModels, disabledModels, alreadyUsed, search]);

	const groupedModels = useMemo(() => {
		const groups = new Map<string, typeof available>();
		for (const m of available) {
			const existing = groups.get(m.provider_name);
			if (existing) {
				existing.push(m);
			} else {
				groups.set(m.provider_name, [m]);
			}
		}
		return groups;
	}, [available]);

	// Derive effective collapsed set: only keep entries for providers currently in view
	const effectiveCollapsed = useMemo(() => {
		const result = new Set<string>();
		for (const p of collapsedProviders) {
			if (groupedModels.has(p)) result.add(p);
		}
		return result;
	}, [collapsedProviders, groupedModels]);

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

	return (
		<div className="flex flex-col h-full min-h-0">
			<p className="text-xs text-amber-400 mb-2 shrink-0">
				{t("arena.swapPicker.title")}
			</p>
			<div className="flex items-center gap-2 shrink-0">
				<FilterInput
					value={search}
					onChange={setSearch}
					placeholder={t("arena.swapPicker.searchPlaceholder")}
					className="w-full max-w-xs mb-2"
				/>
				{groupedModels.size > 0 && (
					<button
						type="button"
						onClick={
							effectiveCollapsed.size === groupedModels.size
								? expandAll
								: collapseAll
						}
						title={
							effectiveCollapsed.size === groupedModels.size
								? t("arena.swapPicker.expandAll")
								: t("arena.swapPicker.collapseAll")
						}
						className="text-white/70 hover:text-(--accent) transition-colors p-1 flex items-center"
					>
						{effectiveCollapsed.size === groupedModels.size ? (
							<ChevronsUpDown size={13} />
						) : (
							<ChevronsDownUp size={13} />
						)}
					</button>
				)}
			</div>
			<div className="flex flex-col gap-1 overflow-y-auto w-full px-2 min-h-0 flex-1">
				{[...groupedModels].map(([providerName, providerModels]) => {
					const isCollapsed = effectiveCollapsed.has(providerName);
					return (
						<div key={providerName}>
							<button
								type="button"
								onClick={() => toggleCollapse(providerName)}
								className="flex items-center gap-1.5 w-full text-[10px] font-medium transition-colors text-(--text-secondary) hover:text-(--text-primary)"
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
								<div className="flex flex-wrap gap-0.5 pl-5 overflow-hidden">
									{providerModels.map((m) => {
										const id = proxyModelID(m.provider_name, m.model_id);
										return (
											<button
												type="button"
												key={id}
												onClick={() => onSelect(id)}
												className="ui-tab px-2 py-0.5 text-[11px] border bg-(--surface-hover) border-(--border-subtle) text-(--text-secondary) hover:text-(--text-primary) hover:border-(--accent)/40 transition-colors"
											>
												{m.display_name || m.model_id}
											</button>
										);
									})}
								</div>
							</div>
						</div>
					);
				})}
				{available.length === 0 && (
					<span className="text-xs text-(--text-muted)">
						{t("arena.swapPicker.noModels")}
					</span>
				)}
			</div>
		</div>
	);
}
