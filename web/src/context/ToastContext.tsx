import {
	createContext,
	type ReactNode,
	useCallback,
	useContext,
	useEffect,
	useLayoutEffect,
	useRef,
	useState,
} from "react";
import { useTranslation } from "react-i18next";
import { useLocalStorage } from "../hooks/useLocalStorage";

type ToastType = "success" | "error" | "info" | "warning";

export type ToastPosition =
	| "top-left"
	| "top-center"
	| "top-right"
	| "bottom-left"
	| "bottom-center"
	| "bottom-right";

interface Toast {
	id: number;
	message: string;
	type: ToastType;
}

interface ToastContextType {
	toast: (message: string, type?: ToastType) => void;
	position: ToastPosition;
	setPosition: (position: ToastPosition) => void;
	timeout: number;
	setTimeout: (timeout: number) => void;
}

// eslint-disable-next-line react-refresh/only-export-components
export const ToastContext = createContext<ToastContextType>({
	toast: () => {},
	position: "bottom-center",
	setPosition: () => {},
	timeout: 4000,
	setTimeout: () => {},
});

let nextId = 0;

const POSITION_CLASSES: Record<ToastPosition, string> = {
	"top-left": "fixed top-4 left-4",
	"top-center": "fixed top-4 left-1/2 -translate-x-1/2",
	"top-right": "fixed top-4 right-4",
	"bottom-left": "fixed bottom-4 left-4",
	"bottom-center": "fixed bottom-4 left-1/2 -translate-x-1/2",
	"bottom-right": "fixed bottom-4 right-4",
};

const ALIGN_CLASSES: Record<ToastPosition, string> = {
	"top-left": "items-start",
	"top-center": "items-center",
	"top-right": "items-end",
	"bottom-left": "items-start",
	"bottom-center": "items-center",
	"bottom-right": "items-end",
};

export function ToastProvider({ children }: { children: ReactNode }) {
	const [toasts, setToasts] = useState<Toast[]>([]);
	const [position, setPosition] = useLocalStorage<ToastPosition>(
		"toastPosition",
		"bottom-center",
		{
			deserialize: (v) => {
				const valid = [
					"top-left",
					"top-center",
					"top-right",
					"bottom-left",
					"bottom-center",
					"bottom-right",
				];
				return valid.includes(v) ? (v as ToastPosition) : "bottom-center";
			},
		},
	);

	const [timeout, setTimeoutValue] = useLocalStorage<number>(
		"toastTimeout",
		4000,
		{
			serialize: (v) => String(Math.min(30000, Math.max(1000, v))),
			deserialize: (v) => {
				const parsed = parseInt(v, 10);
				if (!Number.isNaN(parsed) && parsed >= 1000 && parsed <= 30000)
					return parsed;
				return 4000;
			},
		},
	);

	const addToast = useCallback(
		(message: string, type: ToastType = "success") => {
			const id = nextId++;
			setToasts((prev) => [
				...prev.filter((t) => t.message !== message),
				{ id, message, type },
			]);
		},
		[],
	);

	const removeToast = useCallback((id: number) => {
		setToasts((prev) => prev.filter((t) => t.id !== id));
	}, []);

	const containerClass = POSITION_CLASSES[position];
	const alignClass = ALIGN_CLASSES[position];

	return (
		<ToastContext.Provider
			value={{
				toast: addToast,
				position,
				setPosition,
				timeout,
				setTimeout: setTimeoutValue,
			}}
		>
			{children}
			<div
				className={`${containerClass} z-50 flex flex-col ${alignClass} gap-2`}
			>
				{toasts.map((t) => (
					<ToastItem
						key={t.id}
						toast={t}
						timeout={timeout}
						onDone={() => removeToast(t.id)}
					/>
				))}
			</div>
		</ToastContext.Provider>
	);
}

function ToastItem({
	toast,
	timeout,
	onDone,
}: {
	toast: Toast;
	timeout: number;
	onDone: () => void;
}) {
	const [paused, setPaused] = useState(false);
	const [fading, setFading] = useState(false);
	const startTimeRef = useRef(Date.now());
	const remainingRef = useRef(timeout);
	const timerRef = useRef<ReturnType<typeof setTimeout>>(undefined);
	const btnRef = useRef<HTMLButtonElement>(null);
	const [perimeter, setPerimeter] = useState(0);

	const triggerDone = useCallback(() => {
		setFading(true);
	}, []);

	const handleAnimationEnd = useCallback(() => {
		onDone();
	}, [onDone]);

	const startTimer = useCallback(
		(remaining: number) => {
			clearTimeout(timerRef.current);
			startTimeRef.current = Date.now();
			remainingRef.current = remaining;
			timerRef.current = setTimeout(triggerDone, remaining);
		},
		[triggerDone],
	);

	useEffect(() => {
		startTimer(timeout);
		return () => clearTimeout(timerRef.current);
	}, [timeout, startTimer]);

	// Measure actual button size and compute rounded-rect perimeter
	useLayoutEffect(() => {
		const el = btnRef.current;
		if (!el) return;
		const compute = () => {
			const { width, height } = el.getBoundingClientRect();
			// border-radius matches rounded-md (6px), min of 50% for very small toasts
			const r = Math.min(6, width / 2, height / 2);
			const perim =
				2 * (width - 2 * r) + 2 * (height - 2 * r) + 2 * Math.PI * r;
			setPerimeter(perim);
		};
		compute();
		const ro = new ResizeObserver(compute);
		ro.observe(el);
		return () => ro.disconnect();
	}, []);

	const handleMouseEnter = () => {
		setPaused(true);
		clearTimeout(timerRef.current);
		const elapsed = Date.now() - startTimeRef.current;
		remainingRef.current = Math.max(0, remainingRef.current - elapsed);
	};

	const handleMouseLeave = () => {
		setPaused(false);
		startTimer(remainingRef.current);
	};

	const strokeColors: Record<ToastType, string> = {
		success: "#6ee7b7",
		error: "#fca5a5",
		info: "#cbd5e1",
		warning: "#fde68a",
	};

	const bgColors = {
		success: "bg-emerald-900/70 text-emerald-200",
		error: "bg-red-900/70 text-red-200",
		info: "bg-slate-700/80 text-slate-200",
		warning: "bg-amber-900/70 text-amber-200",
	};

	const handleClick = () => {
		if (toast.type === "error") {
			navigator.clipboard.writeText(toast.message).catch(() => {});
		}
		onDone();
	};

	const { t } = useTranslation();

	return (
		<button
			ref={btnRef}
			type="button"
			onClick={handleClick}
			onMouseEnter={handleMouseEnter}
			onMouseLeave={handleMouseLeave}
			{...(toast.type === "error"
				? { title: t("context.toast.clickToCopyDismiss") }
				: {})}
			className={`relative px-4 py-2 rounded-md shadow-lg text-sm font-medium cursor-pointer hover:brightness-125 whitespace-pre-line text-left border-0 ${bgColors[toast.type]} ${fading ? "opacity-0 translate-y-1" : "opacity-100"}`}
			style={{
				overflow: "hidden",
				transition: "opacity 300ms ease, transform 300ms ease",
			}}
			onTransitionEnd={fading ? handleAnimationEnd : undefined}
		>
			{toast.message}
			{perimeter > 0 && (
				<svg
					aria-hidden="true"
					className="absolute inset-0 w-full h-full pointer-events-none"
				>
					<rect
						x={1}
						y={1}
						width="calc(100% - 2px)"
						height="calc(100% - 2px)"
						rx={5}
						fill="none"
						stroke={strokeColors[toast.type]}
						strokeWidth={2}
						vectorEffect="non-scaling-stroke"
						strokeDasharray={perimeter}
						strokeDashoffset={0}
						strokeLinecap="round"
						style={{
							animation: `toast-fuse ${timeout}ms linear forwards`,
							animationPlayState: paused ? "paused" : "running",
							filter: `drop-shadow(0 0 2px ${strokeColors[toast.type]})`,
							// Override the keyframe's fixed dashoffset with the real perimeter
							// @ts-expect-error CSS custom property for dynamic keyframe
							"--toast-perimeter": perimeter,
						}}
					/>
				</svg>
			)}
		</button>
	);
}

// eslint-disable-next-line react-refresh/only-export-components
export function useToast() {
	return useContext(ToastContext);
}
