/* eslint-disable react-refresh/only-export-components -- splitDuration lives beside the figure that formats with it */

export function splitDuration(ms: number): { value: string; unit: string } {
	if (ms >= 1000) {
		return { value: (ms / 1000).toFixed(2), unit: "s" };
	}
	return { value: String(Math.round(ms)), unit: "ms" };
}

/**
 * A duration rendered as a bold figure with its unit in the muted colour, the
 * shape the performance tiles use.
 */
export function DurationFigure({ ms }: { ms: number }) {
	const d = splitDuration(ms);
	return (
		<>
			{d.value}
			<span className="text-(--text-tertiary)">{d.unit}</span>
		</>
	);
}
