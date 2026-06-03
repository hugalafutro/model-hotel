import { useResizeObserver } from "../hooks/useResizeObserver";

interface FuseOutlineProps {
	/** Stroke color */
	color: string;
	/** Animation duration in milliseconds */
	durationMs: number;
	/** Whether the animation is paused */
	paused?: boolean;
	/** Stroke width (default 1.5) */
	strokeWidth?: number;
	/** Corner radius (default 5) */
	rx?: number;
	/** CSS class for the wrapping SVG */
	className?: string;
}

/**
 * Animated SVG outline that "burns away" like a fuse around its parent element.
 * Used for toast auto-dismiss countdown and circuit breaker cooldown display.
 */
export function FuseOutline({
	color,
	durationMs,
	paused = false,
	strokeWidth = 1.5,
	rx = 5,
	className = "absolute inset-0 w-full h-full pointer-events-none",
}: FuseOutlineProps) {
	const { ref, width = 0, height = 0 } = useResizeObserver();

	// Compute perimeter of the rounded rect
	const w = width - 2;
	const h = height - 2;
	const perimeter =
		w > 0 && h > 0 ? 2 * (w - 2 * rx) + 2 * (h - 2 * rx) + 2 * Math.PI * rx : 0;

	if (perimeter <= 0 || durationMs <= 0) return null;

	return (
		<svg ref={ref} aria-hidden="true" className={className}>
			<rect
				x={1}
				y={1}
				width="calc(100% - 2px)"
				height="calc(100% - 2px)"
				rx={rx}
				fill="none"
				stroke={color}
				strokeWidth={strokeWidth}
				vectorEffect="non-scaling-stroke"
				strokeDasharray={perimeter}
				strokeDashoffset={0}
				strokeLinecap="round"
				style={{
					animation: `fuse ${durationMs}ms linear forwards`,
					animationPlayState: paused ? "paused" : "running",
					filter: `drop-shadow(0 0 2px ${color})`,
					// Override the keyframe's dashoffset with the real perimeter
					// @ts-expect-error CSS custom property for dynamic keyframe
					"--fuse-perimeter": perimeter,
				}}
			/>
		</svg>
	);
}
