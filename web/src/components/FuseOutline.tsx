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
	const { ref, width = 0, height = 0 } = useResizeObserver<SVGSVGElement>();

	// Compute perimeter of the rounded rect
	const w = width - 2;
	const h = height - 2;
	const perimeter =
		w > 0 && h > 0 ? 2 * (w - 2 * rx) + 2 * (h - 2 * rx) + 2 * Math.PI * rx : 0;

	if (durationMs <= 0) return null;

	// Always render the SVG so the ResizeObserver has an element to measure.
	// On first render width/height are 0, so the rect is hidden. Once the
	// observer fires and dimensions are set, the rect becomes visible.
	const showRect = perimeter > 0;

	return (
		<svg
			ref={ref}
			aria-hidden="true"
			className={className}
			viewBox={showRect ? `0 0 ${width} ${height}` : undefined}
		>
			{showRect && (
				<rect
					x={1}
					y={1}
					width={width - 2}
					height={height - 2}
					rx={rx}
					fill="none"
					stroke={color}
					strokeWidth={strokeWidth}
					vectorEffect="non-scaling-stroke"
					strokeDasharray={perimeter}
					strokeLinecap="round"
					style={{
						animation: `fuse ${durationMs}ms linear forwards`,
						animationPlayState: paused ? "paused" : "running",
						filter: `drop-shadow(0 0 2px ${color})`,
						// @ts-expect-error CSS custom property for dynamic keyframe
						"--fuse-perimeter": perimeter,
					}}
				/>
			)}
		</svg>
	);
}
