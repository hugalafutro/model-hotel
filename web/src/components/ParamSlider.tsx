export interface ParamSliderProps {
	label: string;
	value: number | undefined;
	min: number;
	max: number;
	step: number;
	onChange: (v: number | undefined) => void;
}

export function ParamSlider({
	label,
	value,
	min,
	max,
	step,
	onChange,
}: ParamSliderProps) {
	const isSet = value !== undefined;
	const pct = isSet ? ((value - min) / (max - min)) * 100 : 0;
	return (
		<div>
			<div className="flex items-center justify-between">
				<span className="text-[10px] text-(--text-tertiary) uppercase tracking-wider">
					{label}
				</span>
				<input
					type="number"
					value={isSet ? value : ""}
					min={min}
					max={max}
					step={step}
					onChange={(e) => {
						const v = e.target.value;
						if (v === "" || v === "-" || v === ".") {
							onChange(undefined);
							return;
						}
						const n = parseFloat(v);
						if (!Number.isNaN(n)) onChange(n);
					}}
					placeholder="off"
					className="w-14 text-right px-1.5 py-0.5 rounded bg-(--surface-input) text-[10px] text-(--text-primary) border border-transparent focus:border-(--accent) outline-none placeholder:text-(--text-tertiary) no-spinner"
				/>
			</div>
			<input
				type="range"
				min={min}
				max={max}
				step={step}
				value={isSet ? value : min}
				data-set={isSet ? "true" : undefined}
				onChange={(e) => onChange(parseFloat(e.target.value))}
				className="gen-slider w-full h-1 rounded-lg appearance-none cursor-pointer bg-(--surface-hover) accent-(--accent) mt-0.5"
				style={{
					background: isSet
						? `linear-gradient(to right, var(--accent) ${pct}%, var(--surface-hover) ${pct}%)`
						: undefined,
				}}
			/>
		</div>
	);
}
