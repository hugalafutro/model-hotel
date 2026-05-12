import { useEffect, useRef, useState } from "react";
import { dropTrailingZero } from "./utils";

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

	useEffect(() => {
		fromRef.current = display;
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
			setDisplay(current);
			if (p < 1) raf = requestAnimationFrame(tick);
		};

		raf = requestAnimationFrame(tick);
		return () => cancelAnimationFrame(raf);
	}, [value, duration, display]);

	const formatted = formatter
		? formatter(display)
		: dropTrailingZero(display, decimals);
	return (
		<span data-testid="animated-value" style={{ textTransform: "none" }}>
			{formatted}
			{suffix && (
				<span
					className="text-sm font-normal text-(--text-muted) ml-1"
					style={{ textTransform: "none" }}
				>
					{suffix}
				</span>
			)}
		</span>
	);
}
