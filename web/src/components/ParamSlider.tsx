import { useState } from "react";

export interface ParamSliderProps {
	label: string;
	value: number | undefined;
	min: number;
	max: number;
	step: number;
	onChange: (v: number | undefined) => void;
	disabled?: boolean;
	disabledReason?: string;
}

export function ParamSlider({
	label,
	value,
	min,
	max,
	step,
	onChange,
	disabled = false,
	disabledReason,
}: ParamSliderProps) {
	const [showTooltip, setShowTooltip] = useState(false);
	const isSet = value !== undefined;
	const pct = isSet ? ((value - min) / (max - min)) * 100 : 0;
	return (
		// biome-ignore lint/a11y/noStaticElementInteractions: hover tooltip on disabled slider; keyboard users get native title attr on input
		<div
			onMouseEnter={() => disabled && disabledReason && setShowTooltip(true)}
			onMouseLeave={() => setShowTooltip(false)}
		>
			<div className="flex items-center justify-between">
				<span
					className={`text-[10px] uppercase tracking-wider ${
						disabled ? "text-(--text-tertiary)/50" : "text-(--text-tertiary)"
					}`}
				>
					{label}
				</span>
				<input
					type="number"
					value={isSet ? value : ""}
					min={min}
					max={max}
					step={step}
					onChange={(e) => {
						if (disabled) return;
						const v = e.target.value;
						if (v === "" || v === "-" || v === ".") {
							onChange(undefined);
							return;
						}
						const n = parseFloat(v);
						if (!Number.isNaN(n)) onChange(n);
					}}
					placeholder="off"
					disabled={disabled}
					className={`w-14 text-right px-1.5 py-0.5 rounded text-[10px] border border-transparent outline-none placeholder:text-(--text-tertiary) no-spinner ${
						disabled
							? "bg-(--surface-input)/40 text-(--text-tertiary)/50 cursor-not-allowed placeholder:text-(--text-tertiary)/30"
							: "bg-(--surface-input) text-(--text-primary) focus:border-(--accent)"
					}`}
				/>
			</div>
			<div className="relative">
				<input
					type="range"
					min={min}
					max={max}
					step={step}
					value={isSet ? value : min}
					data-set={isSet ? "true" : undefined}
					onChange={(e) => {
						if (disabled) return;
						onChange(parseFloat(e.target.value));
					}}
					disabled={disabled}
					title={disabled && disabledReason ? disabledReason : undefined}
					className={`gen-slider w-full h-1 rounded-lg appearance-none mt-0.5 ${
						disabled ? "cursor-not-allowed opacity-40" : "cursor-pointer"
					} bg-(--surface-hover) accent-(--accent)`}
					style={{
						background: isSet
							? `linear-gradient(to right, var(--accent) ${pct}%, var(--surface-hover) ${pct}%)`
							: undefined,
					}}
				/>
				{showTooltip && disabledReason && (
					<div
						role="tooltip"
						className="absolute left-0 top-full mt-1 z-50 px-2 py-1 rounded text-[10px] text-white bg-gray-800 shadow-lg max-w-[200px] whitespace-normal pointer-events-none"
					>
						{disabledReason}
					</div>
				)}
			</div>
		</div>
	);
}
