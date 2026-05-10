import { useMemo, useState } from "react";
import { FilterInput } from "../../components/FilterInput";
import { proxyModelID } from "../../utils/model";
import type { SwapPickerProps } from "./types";

export function SwapPicker({
	enabledModels,
	disabledModels,
	alreadyUsed,
	onSelect,
}: SwapPickerProps) {
	const [search, setSearch] = useState("");

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

	return (
		<div className="flex flex-col h-full min-h-0">
			<p className="text-xs text-amber-400 mb-2 shrink-0">
				Pick a replacement model
			</p>
			<FilterInput
				value={search}
				onChange={setSearch}
				placeholder="Search models…"
				className="w-full max-w-xs mb-2 shrink-0"
			/>
			<div className="flex flex-wrap gap-1 overflow-y-auto w-full justify-center content-start px-2 min-h-0 flex-1">
				{available.map((m) => {
					const id = proxyModelID(m.provider_name, m.model_id);
					return (
						<button
							type="button"
							key={id}
							onClick={() => onSelect(id)}
							className="px-2 py-0.5 text-[11px] rounded-md border bg-(--surface-hover) border-(--border-subtle) text-(--text-secondary) hover:text-(--text-primary) hover:border-(--accent)/40 transition-colors cursor-pointer"
						>
							{m.display_name || m.model_id}
						</button>
					);
				})}
				{available.length === 0 && (
					<span className="text-xs text-(--text-muted)">
						No models available
					</span>
				)}
			</div>
		</div>
	);
}
