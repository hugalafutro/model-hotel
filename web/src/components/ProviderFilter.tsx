import { Check, ChevronDown, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";

interface Provider {
	id: string;
	name: string;
}

interface ProviderFilterProps {
	providers?: Provider[];
	selected: Set<string>;
	onChange: (selected: Set<string>) => void;
}

export function ProviderFilter({
	providers,
	selected,
	onChange,
}: ProviderFilterProps) {
	const [open, setOpen] = useState(false);
	const [search, setSearch] = useState("");
	const containerRef = useRef<HTMLDivElement>(null);
	const searchRef = useRef<HTMLInputElement>(null);

	const filtered = (
		providers?.filter((p) =>
			p.name.toLowerCase().includes(search.toLowerCase()),
		) ?? []
	).sort((a, b) => a.name.localeCompare(b.name));

	const toggle = (id: string) => {
		const next = new Set(selected);
		if (next.has(id)) next.delete(id);
		else next.add(id);
		onChange(next);
	};

	const clear = () => onChange(new Set());

	const selectAllVisible = () => {
		const next = new Set(selected);
		for (const p of filtered) next.add(p.id);
		onChange(next);
	};

	const deselectAllVisible = () => {
		const next = new Set(selected);
		for (const p of filtered) next.delete(p.id);
		onChange(next);
	};

	useEffect(() => {
		if (!open) return;
		const handle = (e: MouseEvent) => {
			if (
				containerRef.current &&
				!containerRef.current.contains(e.target as Node)
			) {
				setOpen(false);
				setSearch("");
			}
		};
		document.addEventListener("mousedown", handle);
		return () => document.removeEventListener("mousedown", handle);
	}, [open]);

	useEffect(() => {
		if (open && searchRef.current) {
			const t = setTimeout(() => searchRef.current?.focus(), 10);
			return () => clearTimeout(t);
		}
	}, [open]);

	const triggerLabel =
		selected.size === 0
			? "Filter Providers"
			: selected.size === 1
				? (providers?.find((p) => p.id === Array.from(selected)[0])?.name ??
					"1 provider")
				: `${selected.size} providers`;

	return (
		<div
			ref={containerRef}
			data-testid="provider-filter"
			className="relative inline-block w-full"
		>
			<button
				type="button"
				onClick={() => setOpen((v) => !v)}
				className="ui-input text-xs py-1.5 px-2.5 h-9 w-full flex items-center justify-between gap-2 cursor-pointer"
			>
				<span
					className={`truncate ${selected.size === 0 ? "text-(--text-tertiary)" : "text-(--text-primary)"}`}
				>
					{triggerLabel}
				</span>
				<span className="flex items-center gap-1 shrink-0">
					{selected.size > 0 && (
						// biome-ignore lint/a11y/useSemanticElements: cannot use <button> inside <button>
						<span
							role="button"
							tabIndex={0}
							className="inline-flex items-center justify-center w-4 h-4 rounded-full text-[10px] font-medium bg-(--accent-light) text-(--accent) cursor-pointer"
							onClick={(e) => {
								e.stopPropagation();
								clear();
							}}
							onKeyDown={(e) => {
								if (e.key === "Enter" || e.key === " ") {
									e.preventDefault();
									e.stopPropagation();
									clear();
								}
							}}
							title="Clear filter"
						>
							{selected.size}
						</span>
					)}
					<ChevronDown
						size={14}
						className={`text-(--text-tertiary) transition-transform ${open ? "rotate-180" : ""}`}
					/>
				</span>
			</button>

			{open && (
				<div
					data-testid="provider-filter-dropdown"
					className="absolute z-50 mt-1 w-full min-w-50 ui-card py-1 shadow-lg overflow-hidden"
					style={{
						border: "1px solid var(--border-default)",
						backgroundColor: "rgb(14, 14, 20)",
					}}
				>
					{/* Search */}
					<div className="px-2 pt-1 pb-1.5">
						<div className="relative">
							<input
								ref={searchRef}
								type="text"
								value={search}
								onChange={(e) => setSearch(e.target.value)}
								placeholder="Search providers…"
								aria-label="Search providers"
								className="ui-input text-xs h-8 pl-2! pr-7! w-full"
								style={{
									fontFamily: "var(--font-mono), ui-monospace, monospace",
								}}
								onKeyDown={(e) => {
									if (e.key === "Escape") {
										setSearch("");
										setOpen(false);
									}
								}}
							/>
							{search && (
								<button
									type="button"
									onClick={() => setSearch("")}
									className="absolute right-2 top-1/2 -translate-y-1/2 text-(--text-muted) hover:text-(--text-primary)"
									aria-label="Clear search"
								>
									<X size={12} />
								</button>
							)}
						</div>
					</div>

					{/* Bulk actions */}
					{filtered.length > 0 && (
						<div className="flex items-center justify-between px-2.5 pb-1">
							<button
								type="button"
								onClick={selectAllVisible}
								className="text-[11px] text-(--text-tertiary) hover:text-(--accent) transition-colors"
							>
								Select all
							</button>
							<button
								type="button"
								onClick={deselectAllVisible}
								className="text-[11px] text-(--text-tertiary) hover:text-(--text-primary) transition-colors"
							>
								Clear
							</button>
						</div>
					)}

					{/* List */}
					<div className="max-h-48 overflow-y-auto px-1">
						{filtered.length === 0 ? (
							<div
								className="px-2.5 py-3 text-xs text-(--text-muted) text-center"
								style={{
									fontFamily: "var(--font-mono), ui-monospace, monospace",
								}}
							>
								No providers found
							</div>
						) : (
							filtered.map((provider) => {
								const isSelected = selected.has(provider.id);
								return (
									<button
										key={provider.id}
										type="button"
										onClick={() => toggle(provider.id)}
										className={`w-full flex items-center gap-2 px-2 py-1.5 rounded-xl text-xs text-left transition-colors cursor-pointer ${isSelected ? "bg-(--accent-light) text-(--accent)" : "text-(--text-secondary) hover:bg-(--surface-hover)"}`}
										style={{
											fontFamily: "var(--font-mono), ui-monospace, monospace",
										}}
									>
										<span
											className={`inline-flex items-center justify-center w-3.5 h-3.5 rounded border transition-colors shrink-0 ${isSelected ? "bg-(--accent) border-(--accent)" : "border-(--border-input) bg-(--surface-input)"}`}
										>
											{isSelected && <Check size={10} className="text-white" />}
										</span>
										<span className="truncate">{provider.name}</span>
									</button>
								);
							})
						)}
					</div>
				</div>
			)}
		</div>
	);
}
