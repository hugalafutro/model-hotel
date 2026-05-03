import { Check, ChevronDown, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";

interface FilterDropdownProps {
	options: { value: string; label: string; count?: number }[];
	value: string;
	onChange: (value: string) => void;
	placeholder?: string;
	allLabel?: string;
	className?: string;
}

export function FilterDropdown({
	options,
	value,
	onChange,
	placeholder = "Filter",
	allLabel = "All",
	className = "",
}: FilterDropdownProps) {
	const [open, setOpen] = useState(false);
	const containerRef = useRef<HTMLDivElement>(null);

	useEffect(() => {
		if (!open) return;
		const handle = (e: MouseEvent) => {
			if (
				containerRef.current &&
				!containerRef.current.contains(e.target as Node)
			) {
				setOpen(false);
			}
		};
		document.addEventListener("mousedown", handle);
		return () => document.removeEventListener("mousedown", handle);
	}, [open]);

	const selectedOption = options.find((o) => o.value === value);
	const triggerLabel = value ? (selectedOption?.label ?? value) : placeholder;

	return (
		<div ref={containerRef} className={`relative inline-block ${className}`}>
			<button
				type="button"
				onClick={() => setOpen((v) => !v)}
				className="ui-input text-xs py-1.5 px-2.5 h-9 w-full flex items-center justify-between gap-2 cursor-pointer"
			>
				<span
					className={`truncate ${value === "" ? "text-(--text-muted)" : "text-(--text-primary)"}`}
				>
					{triggerLabel}
				</span>
				<span className="flex items-center gap-1 shrink-0">
					{value !== "" && (
						<button
							type="button"
							className="inline-flex items-center justify-center w-4 h-4 rounded-full text-[10px] font-medium bg-(--accent-light) text-(--accent) hover:bg-(--accent) hover:text-white transition-colors"
							onClick={(e) => {
								e.stopPropagation();
								onChange("");
							}}
							title="Clear filter"
						>
							<X size={10} />
						</button>
					)}
					<ChevronDown
						size={14}
						className={`text-(--text-tertiary) transition-transform ${open ? "rotate-180" : ""}`}
					/>
				</span>
			</button>

			{open && (
				<div
					className="absolute z-50 mt-1 min-w-36 ui-card py-1 shadow-lg"
					style={{
						border: "1px solid var(--border-default)",
					}}
				>
					<div className="max-h-48 overflow-y-auto px-1">
						{/* All option */}
						<button
							type="button"
							onClick={() => {
								onChange("");
								setOpen(false);
							}}
							className={`w-full flex items-center gap-2 px-2 py-1.5 rounded text-xs text-left transition-colors cursor-pointer ${value === "" ? "bg-(--accent-light) text-(--accent)" : "text-(--text-secondary) hover:bg-(--surface-hover)"}`}
							style={{
								fontFamily: "var(--font-mono), ui-monospace, monospace",
							}}
						>
							<span
								className={`inline-flex items-center justify-center w-3.5 h-3.5 rounded border transition-colors shrink-0 ${value === "" ? "bg-(--accent) border-(--accent)" : "border-(--border-input) bg-(--surface-input)"}`}
							>
								{value === "" && <Check size={10} className="text-white" />}
							</span>
							<span className="truncate">{allLabel}</span>
						</button>

						{options.map((option) => {
							const isSelected = value === option.value;
							return (
								<button
									key={option.value}
									type="button"
									onClick={() => {
										onChange(option.value);
										setOpen(false);
									}}
									className={`w-full flex items-center gap-2 px-2 py-1.5 rounded text-xs text-left transition-colors cursor-pointer ${isSelected ? "bg-(--accent-light) text-(--accent)" : "text-(--text-secondary) hover:bg-(--surface-hover)"}`}
									style={{
										fontFamily: "var(--font-mono), ui-monospace, monospace",
									}}
								>
									<span
										className={`inline-flex items-center justify-center w-3.5 h-3.5 rounded border transition-colors shrink-0 ${isSelected ? "bg-(--accent) border-(--accent)" : "border-(--border-input) bg-(--surface-input)"}`}
									>
										{isSelected && <Check size={10} className="text-white" />}
									</span>
									<span className="truncate">
										{option.label}
										{option.count !== undefined && (
											<span className="text-(--text-tertiary) ml-1">
												({option.count})
											</span>
										)}
									</span>
								</button>
							);
						})}
					</div>
				</div>
			)}
		</div>
	);
}
