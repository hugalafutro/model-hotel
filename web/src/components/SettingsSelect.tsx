import type { LucideIcon } from "lucide-react";

export interface SelectOption {
	value: string;
	label: string;
}

export interface SettingsSelectProps {
	id: string;
	label: string;
	value: string;
	options: SelectOption[];
	onChange: (value: string) => void;
	description?: React.ReactNode;
	icon?: LucideIcon;
	disabled?: boolean;
}

export function SettingsSelect({
	id,
	label,
	value,
	options,
	onChange,
	description,
	icon: Icon,
	disabled = false,
}: SettingsSelectProps) {
	const isCustomValue =
		value !== "" && !options.some((opt) => opt.value === value);

	return (
		<div>
			<label
				htmlFor={id}
				className="block text-sm font-medium text-gray-300 mb-2"
			>
				{Icon && <Icon size={14} className="inline mr-1.5" />}
				{label}
			</label>
			{isCustomValue ? (
				<input
					id={id}
					type="text"
					value={value}
					onChange={(e) => onChange(e.target.value)}
					className="ui-input"
					disabled={disabled}
				/>
			) : (
				<select
					id={id}
					value={value}
					onChange={(e) => onChange(e.target.value)}
					className="ui-input"
					disabled={disabled}
				>
					{options.map((opt) => (
						<option key={opt.value} value={opt.value}>
							{opt.label}
						</option>
					))}
				</select>
			)}
			{description && (
				<p className="text-gray-500 text-xs mt-1">{description}</p>
			)}
		</div>
	);
}
