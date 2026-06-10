import { useLayoutEffect, useRef } from "react";
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
	/** CSS class for the wrapping SVG. Default positions the SVG as an absolute
	 *  inset-0 overlay that fills its parent — the ResizeObserver measures the
	 *  SVG viewport, which maps to the parent rect because of this positioning.
	 *  If you override className, ensure the SVG still fills the intended target
	 *  element so dimensions are measured correctly. */
	className?: string;
}

/**
 * Animated SVG outline that "burns away" like a fuse around its parent element.
 * Used for toast auto-dismiss countdown and circuit breaker cooldown display.
 *
 * Animation properties (animation name, duration, play state) are set
 * imperatively via refs rather than through React's declarative style prop.
 * This prevents CSS animation restarts when the component re-renders due to
 * parent layout changes (e.g. during drag-and-drop reordering), because React
 * would otherwise re-apply the animation shorthand and reset the animation
 * timeline.
 */
export function FuseOutline({
	color,
	durationMs,
	paused = false,
	strokeWidth = 1.5,
	rx = 5,
	className = "absolute inset-0 w-full h-full pointer-events-none",
}: FuseOutlineProps) {
	const {
		ref: sizeRef,
		width = 0,
		height = 0,
	} = useResizeObserver<SVGSVGElement>();
	const rectRef = useRef<SVGRectElement>(null);

	// Track the last animation string we set imperatively so we only
	// re-set it when durationMs actually changes (which is rare — backed by
	// a useMemo in SortableEntry that only updates on next_retry_at change).
	// Also track the rect element identity so a remounted rect (e.g. after
	// showRect toggles false→true during drag-settle) always gets the animation.
	const lastAnimationRef = useRef<string | undefined>(undefined);
	const lastRectRef = useRef<SVGRectElement | null>(null);

	// Wall-clock bookkeeping so a remounted rect resumes mid-animation
	// instead of restarting from zero (which would make the fuse drift
	// ahead of the real cooldown it visualizes). Paused time is excluded
	// so a toast paused on hover resumes at the right point.
	const animationStartRef = useRef(0);
	const pausedAtRef = useRef<number | null>(null);
	const pausedTotalRef = useRef(0);

	// Compute perimeter of the rounded rect
	const w = Math.max(0, width - 2);
	const h = Math.max(0, height - 2);
	const perimeter =
		w > 0 && h > 0 ? 2 * (w - 2 * rx) + 2 * (h - 2 * rx) + 2 * Math.PI * rx : 0;

	const showRect = perimeter > 0;

	// Set animation properties imperatively (useLayoutEffect fires before
	// browser paint so there's no flash of un-animated rect). Always set the
	// animation when the rect element changes (unmount/remount cycle), or when
	// the animation string changes. Always sync play state since it's safe to
	// toggle without restarting the timeline.
	useLayoutEffect(() => {
		const rect = rectRef.current;
		if (!rect) return;

		const now = performance.now();
		const animationStr = `fuse ${durationMs}ms linear forwards`;
		if (lastAnimationRef.current !== animationStr) {
			// New animation timeline: start from zero. The shorthand resets
			// animation-delay back to 0s.
			lastAnimationRef.current = animationStr;
			lastRectRef.current = rect;
			animationStartRef.current = now;
			pausedAtRef.current = null;
			pausedTotalRef.current = 0;
			rect.style.animation = animationStr;
		} else if (lastRectRef.current !== rect) {
			// Same timeline but the rect remounted (e.g. its size collapsed
			// during a drag-settle): resume mid-animation via a negative
			// delay rather than restarting, so the fuse keeps tracking the
			// real cooldown.
			lastRectRef.current = rect;
			const pausedMs =
				pausedTotalRef.current +
				(pausedAtRef.current !== null ? now - pausedAtRef.current : 0);
			const elapsedMs = now - animationStartRef.current - pausedMs;
			rect.style.animation = animationStr;
			rect.style.animationDelay = `-${elapsedMs}ms`;
		}

		if (paused && pausedAtRef.current === null) {
			pausedAtRef.current = now;
		} else if (!paused && pausedAtRef.current !== null) {
			pausedTotalRef.current += now - pausedAtRef.current;
			pausedAtRef.current = null;
		}
		rect.style.animationPlayState = paused ? "paused" : "running";
	}, [durationMs, paused, showRect]);

	if (durationMs <= 0) return null;

	return (
		<svg
			ref={sizeRef}
			aria-hidden="true"
			className={className}
			viewBox={showRect ? `0 0 ${width} ${height}` : undefined}
		>
			{showRect && (
				<rect
					ref={rectRef}
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
					style={
						{
							filter: `drop-shadow(0 0 2px ${color})`,
							"--fuse-perimeter": perimeter,
						} as React.CSSProperties
					}
				/>
			)}
		</svg>
	);
}
