import type { LucideIcon } from "@/lib/icons";

interface SubModeOption<T extends string> {
	value: T;
	label: string;
	icon: LucideIcon;
}

interface SubModeToggleProps<T extends string> {
	options: [SubModeOption<T>, SubModeOption<T>];
	value: T;
	onChange: (value: T) => void;
	disabled?: boolean;
}

export function SubModeToggle<T extends string>({
	options,
	value,
	onChange,
	disabled = false,
}: SubModeToggleProps<T>) {
	return (
		<div className="flex items-center gap-1">
			{options.map((opt) => {
				const Icon = opt.icon;
				const isActive = value === opt.value;
				return (
					<button
						key={opt.value}
						type="button"
						onClick={() => onChange(opt.value)}
						aria-label={opt.label}
						className={`px-3 py-1 ui-btn text-xs font-medium transition-all ${
							isActive
								? "ui-btn-primary cursor-default"
								: disabled
									? "text-(--text-tertiary) border border-transparent cursor-default"
									: "ui-btn-secondary"
						}`}
					>
						<Icon size={12} className="inline mr-1 -mt-0.5" />
						{opt.label}
					</button>
				);
			})}
		</div>
	);
}
