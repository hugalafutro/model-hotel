import { X } from "lucide-react";

interface FilterInputProps {
	value: string;
	onChange: (value: string) => void;
	placeholder?: string;
	className?: string;
	id?: string;
	autoFocus?: boolean;
	disabled?: boolean;
}

export function FilterInput({
	value,
	onChange,
	placeholder = "Filter…",
	className = "",
	id,
	autoFocus,
	disabled = false,
}: FilterInputProps) {
	return (
		<div className={`relative ${className}`}>
			<input
				id={id}
				type="text"
				placeholder={placeholder}
				value={value}
				onChange={(e) => onChange(e.target.value)}
				disabled={disabled}
				// biome-ignore lint/a11y/noAutofocus: intentional UX — auto-focuses the input when the modal/picker opens
				autoFocus={autoFocus}
				className="ui-input h-9 py-0! w-full pr-7! disabled:opacity-50 disabled:cursor-not-allowed"
			/>
			{value.length > 0 && (
				<button
					type="button"
					onClick={() => onChange("")}
					className="absolute right-2 top-1/2 -translate-y-1/2 text-(--text-tertiary) hover:text-(--text-primary) transition-colors cursor-pointer"
				>
					<X size={14} />
				</button>
			)}
		</div>
	);
}
