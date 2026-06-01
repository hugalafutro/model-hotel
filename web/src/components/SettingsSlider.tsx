import { useCallback } from "react";

export interface SettingsSliderProps {
	id: string;
	label: string;
	value: number;
	min: number;
	max: number;
	step: number;
	/** Auto-clamp step: values snap to multiples of this. E.g. clampStep=5 means values snap to 0,5,10,15... */
	clampStep?: number;
	onChange: (value: number) => void;
	description?: string;
	disabled?: boolean;
	/** Unit suffix displayed after the number in the input, e.g. "ms", "s" */
	unit?: string;
}

function clampToStep(value: number, step: number): number {
	if (!step) return value;
	return Math.round(value / step) * step;
}

export function SettingsSlider({
	id,
	label,
	value,
	min,
	max,
	step,
	clampStep,
	onChange,
	description,
	disabled = false,
	unit,
}: SettingsSliderProps) {
	const pct = ((value - min) / (max - min)) * 100;

	const handleSliderChange = useCallback(
		(e: React.ChangeEvent<HTMLInputElement>) => {
			const raw = Number(e.target.value);
			const clamped = clampStep ? clampToStep(raw, clampStep) : raw;
			onChange(clamped);
		},
		[onChange, clampStep],
	);

	const handleNumberChange = useCallback(
		(e: React.ChangeEvent<HTMLInputElement>) => {
			const raw = Number(e.target.value);
			if (Number.isNaN(raw)) return;
			const clamped = clampStep ? clampToStep(raw, clampStep) : raw;
			onChange(Math.max(min, Math.min(max, clamped)));
		},
		[onChange, min, max, clampStep],
	);

	const handleNumberBlur = useCallback(() => {
		if (clampStep) {
			const clamped = clampToStep(value, clampStep);
			if (clamped !== value) onChange(Math.max(min, Math.min(max, clamped)));
		}
	}, [value, min, max, clampStep, onChange]);

	return (
		<div className={disabled ? "opacity-50 cursor-not-allowed" : ""}>
			<div className="flex items-center gap-3">
				<label
					htmlFor={id}
					className="text-sm font-medium text-gray-300 flex-shrink-0"
				>
					{label}
				</label>
				<input
					type="range"
					id={id}
					min={min}
					max={max}
					step={clampStep || step}
					value={value}
					onChange={handleSliderChange}
					disabled={disabled}
					className={`gen-slider flex-1 h-1 rounded-lg appearance-none ${
						disabled ? "cursor-not-allowed opacity-40" : "cursor-pointer"
					} bg-(--surface-hover) accent-(--accent)`}
					style={{
						background: `linear-gradient(to right, var(--accent) ${pct}%, var(--surface-hover) ${pct}%)`,
					}}
				/>
				<input
					type="number"
					value={value}
					min={min}
					max={max}
					step={clampStep || step}
					onChange={handleNumberChange}
					onBlur={handleNumberBlur}
					disabled={disabled}
					className={`w-14 text-right px-1.5 py-0.5 rounded text-xs border border-transparent outline-none bg-(--surface-input) text-(--text-primary) no-spinner ${
						disabled ? "cursor-not-allowed" : "focus:border-(--accent)"
					}`}
				/>
				{unit ? (
					<span className="text-xs text-(--text-tertiary) -ml-1">{unit}</span>
				) : (
					<span className="text-xs text-transparent -ml-1" aria-hidden>
						w
					</span>
				)}
			</div>
			{description && (
				<p className="text-gray-500 text-xs mt-0.5">{description}</p>
			)}
		</div>
	);
}
