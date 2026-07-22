import {
	createContext,
	type ReactNode,
	useCallback,
	useContext,
	useEffect,
	useRef,
	useState,
} from "react";

export type ToastKind = "success" | "error" | "info";

interface Toast {
	id: number;
	message: string;
	kind: ToastKind;
}

interface ToastApi {
	toast: (message: string, kind?: ToastKind) => void;
}

const ToastContext = createContext<ToastApi | null>(null);

export function ToastProvider({ children }: { children: ReactNode }) {
	const [toasts, setToasts] = useState<Toast[]>([]);
	const nextId = useRef(1);
	// Track pending auto-dismiss timers so they can be cleared on unmount;
	// otherwise a timer firing after teardown calls setToasts on an unmounted
	// tree (in tests, after the jsdom window is gone).
	const timers = useRef<Set<ReturnType<typeof setTimeout>>>(new Set());

	const toast = useCallback((message: string, kind: ToastKind = "info") => {
		const id = nextId.current++;
		setToasts((prev) => [...prev, { id, message, kind }]);
		const handle = setTimeout(() => {
			timers.current.delete(handle);
			setToasts((prev) => prev.filter((t) => t.id !== id));
		}, 5000);
		timers.current.add(handle);
	}, []);

	useEffect(() => {
		const pending = timers.current;
		return () => {
			for (const handle of pending) clearTimeout(handle);
			pending.clear();
		};
	}, []);

	return (
		<ToastContext.Provider value={{ toast }}>
			{children}
			<div className="fd-toasts" role="status" aria-live="polite">
				{toasts.map((t) => (
					<div key={t.id} className={`fd-toast fd-toast-${t.kind}`}>
						{t.message}
					</div>
				))}
			</div>
		</ToastContext.Provider>
	);
}

// eslint-disable-next-line react-refresh/only-export-components
export function useToast(): ToastApi {
	const ctx = useContext(ToastContext);
	if (!ctx) throw new Error("useToast must be used within a ToastProvider");
	return ctx;
}
