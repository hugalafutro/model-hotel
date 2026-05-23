import {
	createContext,
	type ReactNode,
	useCallback,
	useContext,
	useEffect,
	useState,
} from "react";
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
	useEffect(() => {
		const t = setTimeout(onDone, timeout);
		return () => clearTimeout(t);
	}, [onDone, timeout]);

	const colors = {
		success: "bg-emerald-900/70 text-emerald-200 border-emerald-700/60",
		error: "bg-red-900/70 text-red-200 border-red-700/60",
		info: "bg-slate-700/80 text-slate-200 border-slate-600/60",
		warning: "bg-amber-900/70 text-amber-200 border-amber-700/60",
	};

	const handleClick = () => {
		if (toast.type === "error") {
			navigator.clipboard.writeText(toast.message).catch(() => {});
		}
		onDone();
	};

	return (
		<button
			type="button"
			onClick={handleClick}
			title={toast.type === "error" ? "Click to copy and dismiss" : undefined}
			className={`px-4 py-2 rounded-lg shadow-lg border text-sm font-medium cursor-pointer hover:brightness-125 transition-all whitespace-pre-line text-left ${colors[toast.type]}`}
		>
			{toast.message}
		</button>
	);
}

// eslint-disable-next-line react-refresh/only-export-components
export function useToast() {
	return useContext(ToastContext);
}
