import { ChevronDown, ChevronUp } from "lucide-react";
import { useCallback } from "react";

export interface SettingsSliderProps {
	id: string;
	label: string;
	value: number;
	min: number;
	max: number;
	step: number;
	clampStep?: number;
	onChange: (value: number) => void;
	description?: string;
	disabled?: boolean;
	hideUnit?: boolean;
	unit?: string;
	infinityValue?: number;
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
	hideUnit = false,
	infinityValue,
}: SettingsSliderProps) {
	const isInfinity = infinityValue !== undefined && value >= infinityValue;
	const displayValue = isInfinity ? "∞" : value;
	const pct = isInfinity
		? 100
		: max === min
			? 100
			: ((value - min) / (max - min)) * 100;

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

	const stepUp = useCallback(() => {
		if (isInfinity) {
			const firstStep =
				min > (infinityValue ?? 0)
					? min
					: clampToStep(
							(infinityValue ?? 0) + (clampStep || step),
							clampStep || step,
						);
			onChange(Math.min(max, firstStep));
			return;
		}
		const next = Math.min(
			max,
			clampToStep(value + (clampStep || step), clampStep || step),
		);
		onChange(next);
	}, [value, max, min, clampStep, step, onChange, isInfinity, infinityValue]);

	const stepDown = useCallback(() => {
		const next = Math.max(
			min,
			clampToStep(value - (clampStep || step), clampStep || step),
		);
		onChange(next);
	}, [value, min, clampStep, step, onChange]);

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
				<div className="flex items-center gap-px">
					<div className="flex flex-col">
						<button
							type="button"
							onClick={stepUp}
							disabled={disabled || value >= max}
							className="px-1 py-0 text-gray-500 hover:text-(--accent) disabled:opacity-30 disabled:cursor-not-allowed leading-none"
						>
							<ChevronUp size={10} />
						</button>
						<button
							type="button"
							onClick={stepDown}
							disabled={disabled || value <= min}
							className="px-1 py-0 text-gray-500 hover:text-(--accent) disabled:opacity-30 disabled:cursor-not-allowed leading-none"
						>
							<ChevronDown size={10} />
						</button>
					</div>
					<input
						type="number"
						value={displayValue}
						min={min}
						max={max}
						step={clampStep || step}
						onChange={handleNumberChange}
						onBlur={handleNumberBlur}
						disabled={disabled}
						readOnly={isInfinity}
						className={`w-12 text-right px-1 py-0.5 rounded text-xs border border-transparent outline-none bg-(--surface-input) text-(--text-primary) no-spinner ${
							isInfinity ? "text-center" : ""
						} ${disabled ? "cursor-not-allowed" : "focus:border-(--accent)"}`}
					/>
				</div>
				{unit && (
					<span
						className={`text-xs -ml-1 ${hideUnit ? "text-transparent" : "text-(--text-tertiary)"}`}
						aria-hidden={hideUnit}
					>
						{unit}
					</span>
				)}
			</div>
			{description && (
				<p className="text-gray-500 text-xs mt-0.5">{description}</p>
			)}
		</div>
	);
}
