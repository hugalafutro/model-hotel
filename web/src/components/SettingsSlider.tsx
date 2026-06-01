import { ChevronDown, ChevronUp } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";

export interface SettingsSliderProps {
	id: string;
	label: string;
	value: number;
	min: number;
	max: number;
	step: number;
	clampStep?: number;
	onChange: (value: number) => void;
	description?: React.ReactNode;
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
	const [local, setLocal] = useState(value);
	const prevValue = useRef(value);
	const committed = useRef(value);

	useEffect(() => {
		if (prevValue.current !== value) {
			prevValue.current = value;
			committed.current = value;
			setLocal(value);
		}
	}, [value]);

	const isInfinity = infinityValue !== undefined && local === infinityValue;
	const pct = isInfinity
		? 100
		: max === min
			? 100
			: Math.round(((local - min) / (max - min)) * 200) / 2;

	const handleSliderChange = useCallback(
		(e: React.ChangeEvent<HTMLInputElement>) => {
			const raw = Number(e.target.value);
			const clamped = clampStep ? clampToStep(raw, clampStep) : raw;
			setLocal(clamped);
		},
		[clampStep],
	);

	const handleSliderCommit = useCallback(
		(
			e:
				| React.PointerEvent<HTMLInputElement>
				| React.KeyboardEvent<HTMLInputElement>,
		) => {
			const raw = Number(e.currentTarget.value);
			const clamped = clampStep ? clampToStep(raw, clampStep) : raw;
			if (clamped !== committed.current) {
				committed.current = clamped;
				onChange(clamped);
			}
		},
		[onChange, clampStep],
	);

	const handleSliderKeyUp = useCallback(
		(e: React.KeyboardEvent<HTMLInputElement>) => {
			if (
				e.key === "ArrowUp" ||
				e.key === "ArrowDown" ||
				e.key === "ArrowLeft" ||
				e.key === "ArrowRight" ||
				e.key === "Home" ||
				e.key === "End" ||
				e.key === "PageUp" ||
				e.key === "PageDown"
			) {
				handleSliderCommit(e);
			}
		},
		[handleSliderCommit],
	);

	const handleNumberChange = useCallback(
		(e: React.ChangeEvent<HTMLInputElement>) => {
			const raw = Number(e.target.value);
			if (Number.isNaN(raw)) return;
			const clamped = clampStep ? clampToStep(raw, clampStep) : raw;
			const v = Math.max(min, Math.min(max, clamped));
			setLocal(v);
		},
		[min, max, clampStep],
	);

	const handleNumberBlur = useCallback(() => {
		const clamped = clampStep ? clampToStep(local, clampStep) : local;
		const v = Math.max(min, Math.min(max, clamped));
		if (v !== local) setLocal(v);
		if (v !== committed.current) {
			committed.current = v;
			onChange(v);
		}
	}, [local, min, max, clampStep, onChange]);

	const handleNumberKeyDown = useCallback(
		(e: React.KeyboardEvent<HTMLInputElement>) => {
			if (e.key === "Enter") {
				e.currentTarget.blur();
			}
		},
		[],
	);

	const stepUp = useCallback(() => {
		if (isInfinity) {
			const firstStep =
				min > (infinityValue ?? 0)
					? min
					: clampToStep(
							(infinityValue ?? 0) + (clampStep || step),
							clampStep || step,
						);
			const v = Math.min(max, firstStep);
			setLocal(v);
			if (v !== committed.current) {
				committed.current = v;
				onChange(v);
			}
			return;
		}
		const next = Math.min(
			max,
			clampToStep(local + (clampStep || step), clampStep || step),
		);
		setLocal(next);
		if (next !== committed.current) {
			committed.current = next;
			onChange(next);
		}
	}, [local, max, min, clampStep, step, onChange, isInfinity, infinityValue]);

	const stepDown = useCallback(() => {
		const next = Math.max(
			min,
			clampToStep(local - (clampStep || step), clampStep || step),
		);
		setLocal(next);
		if (next !== committed.current) {
			committed.current = next;
			onChange(next);
		}
	}, [local, min, clampStep, step, onChange]);

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
					value={local}
					onChange={handleSliderChange}
					onPointerUp={handleSliderCommit}
					onKeyUp={handleSliderKeyUp}
					disabled={disabled}
					className={`gen-slider flex-1 h-1.5 rounded-lg appearance-none ${
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
							disabled={disabled || local >= max}
							className="px-1 py-0 text-gray-500 hover:text-(--accent) disabled:opacity-30 disabled:cursor-not-allowed leading-none"
						>
							<ChevronUp size={10} />
						</button>
						<button
							type="button"
							onClick={stepDown}
							disabled={disabled || local <= min}
							className="px-1 py-0 text-gray-500 hover:text-(--accent) disabled:opacity-30 disabled:cursor-not-allowed leading-none"
						>
							<ChevronDown size={10} />
						</button>
					</div>
					{isInfinity ? (
						<span
							className={`w-12 text-center inline-block px-1 py-0.5 rounded text-xs bg-(--surface-input) text-(--text-primary) ${
								disabled ? "cursor-not-allowed opacity-50" : ""
							}`}
						>
							∞
						</span>
					) : (
						<input
							type="number"
							value={local}
							min={min}
							max={max}
							step={clampStep || step}
							onChange={handleNumberChange}
							onBlur={handleNumberBlur}
							onKeyDown={handleNumberKeyDown}
							disabled={disabled}
							className={`w-12 text-right px-1 py-0.5 rounded text-xs border border-transparent outline-none bg-(--surface-input) text-(--text-primary) no-spinner ${
								disabled ? "cursor-not-allowed" : "focus:border-(--accent)"
							}`}
						/>
					)}
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
