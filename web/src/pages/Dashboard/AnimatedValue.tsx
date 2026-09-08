import { useEffect, useRef, useState } from "react";
import { dropTrailingZero } from "../../utils/format";

export function AnimatedValue({
	value,
	decimals = 0,
	suffix = "",
	duration = 1200,
	formatter,
}: {
	value: number;
	decimals?: number;
	suffix?: string;
	duration?: number;
	formatter?: (val: number) => string;
}) {
	const [display, setDisplay] = useState(0);
	const startRef = useRef<number | null>(null);
	const fromRef = useRef(0);
	const toRef = useRef(value);
	// The last value put on screen, and the point the next animation eases
	// from. Each frame records it here rather than the effect reading the
	// `display` state: a `display` dependency would cancel and restart the
	// ease on every frame, leaving the value creeping towards the target
	// instead of arriving in `duration` ms.
	const shownRef = useRef(0);

	useEffect(() => {
		fromRef.current = shownRef.current;
		toRef.current = value;
		startRef.current = null;

		let raf: number;
		const ease = (t: number) => 1 - (1 - t) ** 3;

		const tick = (ts: number) => {
			if (startRef.current === null) startRef.current = ts;
			const elapsed = ts - startRef.current;
			const p = Math.min(elapsed / duration, 1);
			const eased = ease(p);
			const current =
				fromRef.current + (toRef.current - fromRef.current) * eased;
			shownRef.current = current;
			setDisplay(current);
			if (p < 1) raf = requestAnimationFrame(tick);
		};

		raf = requestAnimationFrame(tick);
		return () => cancelAnimationFrame(raf);
	}, [value, duration]);

	// Unit suffixes (% s ms) should be tight against the number;
	// word suffixes (T/Rq etc.) get a space.
	const isUnitSuffix = /^(ms|μs|%?s)$/.test(suffix) || suffix === "%";
	const formatted = formatter
		? formatter(display)
		: dropTrailingZero(display, decimals);
	return (
		<span data-testid="animated-value" style={{ textTransform: "none" }}>
			{formatted}
			{suffix && (
				<span
					className={`text-sm font-normal text-(--text-primary) ${isUnitSuffix ? "" : "ml-1"}`}
					style={{ textTransform: "none" }}
				>
					{suffix}
				</span>
			)}
		</span>
	);
}
