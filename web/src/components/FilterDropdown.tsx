import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Check, ChevronDown, X } from "@/lib/icons";

interface FilterDropdownProps {
	options: { value: string; label: string; count?: number }[];
	value: string;
	onChange: (value: string) => void;
	placeholder?: string;
	allLabel?: string;
	className?: string;
	/**
	 * When true (default) the dropdown is a filter: it shows an "All" option and
	 * a clear (X) button that reset the value to "". Set false to use it as a
	 * required selector (e.g. a form field) where every choice is a real value.
	 */
	allowClear?: boolean;
	/**
	 * Trigger sizing. "input" (default) matches a form field (ui-input, h-9).
	 * "compact" drops ui-input for a 10px, minimally-padded trigger that sits
	 * flush with the dashboard's segmented toggles (ui-tab) rather than towering
	 * over them. Only the trigger changes; the popup menu is shared.
	 */
	variant?: "input" | "compact";
}

export function FilterDropdown({
	options,
	value,
	onChange,
	placeholder,
	allLabel,
	className = "",
	allowClear = true,
	variant = "input",
}: FilterDropdownProps) {
	const { t } = useTranslation();
	const effectivePlaceholder =
		placeholder ?? t("components.filterDropdown.placeholder");
	const effectiveAllLabel = allLabel ?? t("components.filterDropdown.allLabel");
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
	const displayLabel = value
		? (selectedOption?.label ?? value)
		: effectiveAllLabel;

	const compact = variant === "compact";
	// Compact avoids ui-input (whose themed base font/padding beat Tailwind
	// utilities) so it can shrink to the 10px, py-px scale of the ui-tab toggles
	// it sits beside; the default keeps the form-field look. w-full lets the
	// button fill (and so truncate within) a width-capped wrapper such as the
	// dashboard's max-w-32 rather than growing to a long owner label.
	const triggerClass = compact
		? "text-[10px] leading-[1.6] font-semibold py-px px-1.5 rounded-(--radius-button) border border-(--border-input) bg-(--surface-input) hover:border-(--accent) transition-colors flex items-center justify-between gap-1 w-full"
		: "ui-input text-xs py-1.5 px-2.5 h-9 w-full flex items-center justify-between gap-2";
	const iconSize = compact ? 12 : 14;

	return (
		<div ref={containerRef} className={`relative inline-block ${className}`}>
			<button
				type="button"
				onClick={() => setOpen((v) => !v)}
				aria-label={
					value
						? `${effectivePlaceholder}: ${displayLabel}`
						: effectivePlaceholder
				}
				className={triggerClass}
			>
				<span
					className={`truncate ${value === "" ? "text-(--text-secondary)" : "text-(--text-primary)"}`}
				>
					{displayLabel}
				</span>
				<span className="flex items-center gap-1 shrink-0">
					{allowClear && value !== "" && (
						// biome-ignore lint/a11y/useSemanticElements: cannot use <button> inside <button>
						<span
							role="button"
							tabIndex={0}
							className="inline-flex items-center justify-center text-(--text-tertiary) hover:text-(--text-primary) transition-colors"
							onClick={(e) => {
								e.stopPropagation();
								onChange("");
							}}
							onKeyDown={(e) => {
								if (e.key === "Enter" || e.key === " ") {
									e.preventDefault();
									e.stopPropagation();
									onChange("");
								}
							}}
							title={t("common.clearFilter")}
						>
							<X size={iconSize} />
						</span>
					)}
					<ChevronDown
						size={iconSize}
						className={`text-(--text-tertiary) transition-transform ${open ? "rotate-180" : ""}`}
					/>
				</span>
			</button>

			{open && (
				<div
					className="absolute z-50 mt-1 min-w-36 ui-card py-1 shadow-lg overflow-hidden"
					style={{
						border: "1px solid var(--border-default)",
					}}
				>
					<div className="max-h-48 overflow-y-auto px-1">
						{/* All option (filter mode only) */}
						{allowClear && (
							<button
								type="button"
								onClick={() => {
									onChange("");
									setOpen(false);
								}}
								className={`w-full flex items-center gap-2 px-2 py-1.5 rounded-(--radius-button) text-xs text-left transition-colors ${value === "" ? "bg-(--accent-light) text-(--accent)" : "text-(--text-secondary) hover:bg-(--surface-hover)"}`}
							>
								<span
									className={`inline-flex items-center justify-center w-3.5 h-3.5 rounded border transition-colors shrink-0 ${value === "" ? "bg-(--accent) border-(--accent)" : "border-(--border-input) bg-(--surface-input)"}`}
								>
									{value === "" && <Check size={10} className="text-white" />}
								</span>
								<span className="truncate">{effectiveAllLabel}</span>
							</button>
						)}

						{options.map((option) => {
							const isSelected = value === option.value;
							return (
								<button
									key={option.value}
									type="button"
									data-value={option.value}
									data-selected={isSelected}
									onClick={() => {
										onChange(option.value);
										setOpen(false);
									}}
									className={`w-full flex items-center gap-2 px-2 py-1.5 rounded-(--radius-button) text-xs text-left transition-colors ${isSelected ? "bg-(--accent-light) text-(--accent)" : "text-(--text-secondary) hover:bg-(--surface-hover)"}`}
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
