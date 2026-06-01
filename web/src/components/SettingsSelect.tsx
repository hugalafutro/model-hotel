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
	disabled?: boolean;
	inline?: boolean;
}

export function SettingsSelect({
	id,
	label,
	value,
	options,
	onChange,
	description,
	disabled = false,
	inline = false,
}: SettingsSelectProps) {
	const isCustomValue =
		value !== "" && !options.some((opt) => opt.value === value);

	const selectElement = isCustomValue ? (
		<input
			id={id}
			type="text"
			value={value}
			onChange={(e) => onChange(e.target.value)}
			className={`ui-input disabled:opacity-50 disabled:cursor-not-allowed ${inline ? "w-auto text-xs px-2 py-1" : ""}`}
			disabled={disabled}
		/>
	) : (
		<select
			id={id}
			value={value}
			onChange={(e) => onChange(e.target.value)}
			className={`ui-input disabled:opacity-50 disabled:cursor-not-allowed ${inline ? "w-auto text-xs px-2 py-1" : ""}`}
			disabled={disabled}
		>
			{options.map((opt) => (
				<option key={opt.value} value={opt.value}>
					{opt.label}
				</option>
			))}
		</select>
	);

	if (inline) {
		return (
			<div>
				<div className="flex items-center justify-between gap-3">
					<label
						htmlFor={id}
						className="text-sm font-medium text-gray-300 whitespace-nowrap"
					>
						{label}
					</label>
					{selectElement}
				</div>
				{description && (
					<p className="text-gray-500 text-xs mt-1">{description}</p>
				)}
			</div>
		);
	}

	return (
		<div>
			<label
				htmlFor={id}
				className="block text-sm font-medium text-gray-300 mb-2"
			>
				{label}
			</label>
			{selectElement}
			{description && (
				<p className="text-gray-500 text-xs mt-1">{description}</p>
			)}
		</div>
	);
}
