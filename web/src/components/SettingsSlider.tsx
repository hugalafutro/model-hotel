import { useCallback, useEffect, useRef, useState } from "react";
import { ChevronDown, ChevronUp } from "@/lib/icons";
import { isForcedBlur } from "../utils/forcedBlur";
import { ResetButton } from "./ResetButton";

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
	/** When present, renders a reset-to-default icon after the label */
	onReset?: () => void;
	/** Tooltip for the reset icon (already i18n'd) */
	resetTooltip?: string;
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
	onReset,
	resetTooltip,
}: SettingsSliderProps) {
	const [local, setLocal] = useState(value);
	const prevValue = useRef(value);
	const committed = useRef(value);

	// A new `value` prop is the authoritative one and replaces the local draft.
	// Kept in an effect rather than adjusted during render: the commit mark is a
	// ref, and refs may not be written while rendering.
	useEffect(() => {
		if (prevValue.current !== value) {
			prevValue.current = value;
			committed.current = value;
			setLocal(value);
		}
	}, [value]);

	const effStep = clampStep || step;
	const clamp = useCallback(
		(v: number) => Math.max(min, Math.min(max, v)),
		[min, max],
	);
	// One write path: the local draft always follows, the parent hears only a
	// real change.
	const commit = useCallback(
		(v: number) => {
			setLocal(v);
			if (v !== committed.current) {
				committed.current = v;
				onChange(v);
			}
		},
		[onChange],
	);

	const isInfinity = infinityValue !== undefined && local === infinityValue;
	const pct = isInfinity
		? 0
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
			commit(clamped);
		},
		[commit, clampStep],
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
			setLocal(clamp(clamped));
		},
		[clamp, clampStep],
	);

	const handleNumberBlur = useCallback(
		(e: React.FocusEvent<HTMLInputElement>) => {
			// A forced blur (the enclosing fieldset went managed while the number
			// had focus) discards the draft instead of committing it: the key is
			// fleet-owned now and the field must show the fleet value.
			if (isForcedBlur(e)) {
				setLocal(committed.current);
				return;
			}
			const clamped = clampStep ? clampToStep(local, clampStep) : local;
			commit(clamp(clamped));
		},
		[local, clamp, commit, clampStep],
	);

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
					: clampToStep((infinityValue ?? 0) + effStep, effStep);
			commit(Math.min(max, firstStep));
			return;
		}
		commit(Math.min(max, clampToStep(local + effStep, effStep)));
	}, [local, max, min, effStep, commit, isInfinity, infinityValue]);

	const stepDown = useCallback(() => {
		commit(Math.max(min, clampToStep(local - effStep, effStep)));
	}, [local, min, effStep, commit]);

	return (
		<div className={disabled ? "opacity-50 cursor-not-allowed" : ""}>
			<div className="flex items-center gap-3">
				<label
					htmlFor={id}
					className="text-sm font-medium text-gray-300 flex-shrink-0"
				>
					{label}
				</label>
				{onReset && (
					<ResetButton
						tooltip={resetTooltip ?? ""}
						onClick={onReset}
						size={12}
						className="inline-flex align-middle"
						disabled={disabled}
					/>
				)}
				<input
					type="range"
					id={id}
					min={min}
					max={max}
					step={effStep}
					value={local}
					onChange={handleSliderChange}
					onPointerUp={handleSliderCommit}
					onKeyUp={handleSliderKeyUp}
					disabled={disabled}
					className={`gen-slider flex-1 min-w-0 h-1.5 rounded-lg appearance-none ${
						disabled ? "cursor-not-allowed" : "cursor-pointer"
					} bg-(--surface-hover) accent-(--accent)`}
					style={{
						background: `linear-gradient(to right, var(--accent) ${pct}%, var(--surface-hover) ${pct}%)`,
					}}
				/>
				<div className="flex items-center gap-px shrink-0">
					<div className="flex flex-col">
						<button
							type="button"
							onClick={stepUp}
							disabled={disabled || local >= max}
							className="ui-icon-btn px-1 py-0 leading-none"
						>
							<ChevronUp size={10} />
						</button>
						<button
							type="button"
							onClick={stepDown}
							disabled={disabled || local <= min}
							className="ui-icon-btn px-1 py-0 leading-none"
						>
							<ChevronDown size={10} />
						</button>
					</div>
					{isInfinity ? (
						<span
							className={`settings-slider-infinity w-12 text-center inline-block px-1 py-0.5 rounded text-xs bg-(--surface-input) text-(--text-primary) ${
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
							step={effStep}
							onChange={handleNumberChange}
							onBlur={handleNumberBlur}
							onKeyDown={handleNumberKeyDown}
							disabled={disabled}
							className={`w-12 text-right px-1 py-0.5 rounded text-xs border border-transparent outline-none bg-(--surface-input) text-(--text-primary) no-spinner disabled:opacity-50 ${
								disabled ? "cursor-not-allowed" : "focus:border-(--accent)"
							}`}
						/>
					)}
				</div>
				{unit && (
					<span
						className={`text-xs -ml-1 shrink-0 ${hideUnit ? "text-transparent" : "text-(--text-tertiary)"}`}
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
