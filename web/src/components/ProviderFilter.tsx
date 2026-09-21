import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Check, ChevronDown, X } from "@/lib/icons";
import { useClickOutside } from "../hooks/useClickOutside";
import { moveOptionFocus } from "../utils/a11y";
import { toggleInSet } from "../utils/collections";
import { sortByName } from "../utils/sort";

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
	const { t } = useTranslation();
	const [open, setOpen] = useState(false);
	const [search, setSearch] = useState("");
	const containerRef = useRef<HTMLDivElement>(null);
	const searchRef = useRef<HTMLInputElement>(null);
	const triggerRef = useRef<HTMLButtonElement>(null);
	const listRef = useRef<HTMLDivElement>(null);

	const filtered = sortByName(
		providers?.filter((p) =>
			p.name.toLowerCase().includes(search.toLowerCase()),
		),
	);

	const toggle = (id: string) => onChange(toggleInSet(selected, id));

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

	useClickOutside(
		containerRef,
		() => {
			setOpen(false);
			setSearch("");
		},
		{ enabled: open },
	);

	useEffect(() => {
		if (open && searchRef.current) {
			const t = setTimeout(() => searchRef.current?.focus(), 10);
			return () => clearTimeout(t);
		}
	}, [open]);

	const triggerLabel =
		selected.size === 0
			? t("components.providerFilter.filterProviders")
			: selected.size === 1
				? (providers?.find((p) => p.id === Array.from(selected)[0])?.name ??
					t("components.providerFilter.provider", { count: 1 }))
				: t("components.providerFilter.provider", {
						count: selected.size,
					});

	return (
		// biome-ignore lint/a11y/noStaticElementInteractions: keyboard dismissal for the popup this wrapper positions
		<div
			ref={containerRef}
			data-testid="provider-filter"
			className="relative inline-block w-full"
			onKeyDown={(e) => {
				if (!open) return;
				if (e.key === "Escape") {
					e.stopPropagation();
					setSearch("");
					setOpen(false);
					// The search box or option that had focus unmounts with the
					// menu; the trigger takes focus back.
					triggerRef.current?.focus();
					return;
				}
				// Arrow keys walk the options, Home/End jump between them; a first
				// ArrowDown from the search box enters the list.
				if (moveOptionFocus(listRef.current, e.key, document.activeElement)) {
					e.preventDefault();
				}
			}}
		>
			<button
				ref={triggerRef}
				type="button"
				onClick={() => setOpen((v) => !v)}
				aria-haspopup="listbox"
				aria-expanded={open}
				className="ui-input text-xs py-1.5 px-2.5 h-9 w-full flex items-center justify-between gap-2"
				// Room for the clear control, which sits beside the trigger rather
				// than inside it (a button cannot contain a button).
				style={selected.size > 0 ? { paddingRight: 48 } : undefined}
			>
				<span
					className={`truncate ${selected.size === 0 ? "text-(--text-tertiary)" : "text-(--text-primary)"}`}
				>
					{triggerLabel}
				</span>
				<ChevronDown
					size={14}
					className={`shrink-0 text-(--text-tertiary) transition-transform ${open ? "rotate-180" : ""}`}
				/>
			</button>
			{selected.size > 0 && (
				<button
					type="button"
					className="ui-badge absolute right-7 top-1/2 -translate-y-1/2 inline-flex items-center justify-center w-4 h-4 text-[10px] font-medium bg-(--accent-light) text-(--accent)"
					onClick={clear}
					aria-label={t("components.providerFilter.clearFilter")}
					title={t("components.providerFilter.clearFilter")}
				>
					{selected.size}
				</button>
			)}

			{open && (
				<div
					data-testid="provider-filter-dropdown"
					className="absolute z-50 mt-1 w-full min-w-50 ui-card py-1 shadow-lg overflow-hidden"
					style={{
						border: "1px solid var(--border-default)",
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
								placeholder={t("components.providerFilter.searchProviders")}
								aria-label={t("components.providerFilter.searchProviders")}
								className="ui-input text-xs h-8 pl-2! pr-7! w-full"
								onKeyDown={(e) => {
									if (e.key === "Escape") {
										setSearch("");
										setOpen(false);
										triggerRef.current?.focus();
									}
								}}
							/>
							{search && (
								<button
									type="button"
									onClick={() => setSearch("")}
									className="absolute right-2 top-1/2 -translate-y-1/2 text-(--text-muted) hover:text-(--text-primary)"
									aria-label={t("common.clearFilter")}
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
								className="ui-link-accent text-[11px] text-(--text-tertiary)"
							>
								{t("components.providerFilter.selectAll")}
							</button>
							<button
								type="button"
								onClick={deselectAllVisible}
								className="text-[11px] text-(--text-tertiary) hover:text-(--text-primary) transition-colors"
							>
								{t("components.providerFilter.clear")}
							</button>
						</div>
					)}

					{/* List */}
					<div
						ref={listRef}
						role="listbox"
						aria-multiselectable="true"
						aria-label={t("components.providerFilter.filterProviders")}
						className="max-h-48 overflow-y-auto px-1"
					>
						{filtered.length === 0 ? (
							<div className="px-2.5 py-3 text-xs text-(--text-muted) text-center">
								{t("components.providerFilter.noProvidersFound")}
							</div>
						) : (
							filtered.map((provider) => {
								const isSelected = selected.has(provider.id);
								return (
									<button
										key={provider.id}
										type="button"
										role="option"
										aria-selected={isSelected}
										onClick={() => toggle(provider.id)}
										className={`w-full flex items-center gap-2 px-2 py-1.5 rounded-(--radius-button) text-xs text-left transition-colors ${isSelected ? "bg-(--accent-light) text-(--accent)" : "text-(--text-secondary) hover:bg-(--surface-hover)"}`}
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
