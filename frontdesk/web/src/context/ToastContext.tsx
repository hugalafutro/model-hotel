import {
	createContext,
	type ReactNode,
	useCallback,
	useContext,
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

	const toast = useCallback((message: string, kind: ToastKind = "info") => {
		const id = nextId.current++;
		setToasts((prev) => [...prev, { id, message, kind }]);
		setTimeout(() => {
			setToasts((prev) => prev.filter((t) => t.id !== id));
		}, 5000);
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
