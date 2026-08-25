import {
	createContext,
	type ReactNode,
	useCallback,
	useContext,
	useEffect,
	useRef,
	useState,
} from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { FuseOutline } from "../components/FuseOutline";
import { useCopyToClipboard } from "../hooks/useCopyToClipboard";
import { useLocalStorage } from "../hooks/useLocalStorage";
import { Copy, X } from "../lib/icons";

export type ToastType = "success" | "error" | "info" | "warning";

export type ToastPosition =
	| "top-left"
	| "top-center"
	| "top-right"
	| "bottom-left"
	| "bottom-center"
	| "bottom-right";

/** An inline control on the toast, e.g. "Undo" on a confirmed dismissal. */
export interface ToastAction {
	label: string;
	onClick: () => void;
}

interface Toast {
	id: number;
	message: string;
	type: ToastType;
	action?: ToastAction;
}

interface ToastContextType {
	toast: (message: string, type?: ToastType, action?: ToastAction) => void;
	position: ToastPosition;
	setPosition: (position: ToastPosition) => void;
	timeout: number;
	setTimeout: (timeout: number) => void;
	fuse: boolean;
	setFuse: (fuse: boolean) => void;
}

// eslint-disable-next-line react-refresh/only-export-components
export const ToastContext = createContext<ToastContextType>({
	toast: () => {},
	position: "bottom-center",
	setPosition: () => {},
	timeout: 4000,
	setTimeout: () => {},
	fuse: true,
	setFuse: () => {},
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

	const [fuse, setFuse] = useLocalStorage<boolean>("toastFuse", true, {
		serialize: (v) => (v ? "true" : "false"),
		deserialize: (v) => v !== "false",
	});

	const addToast = useCallback(
		(message: string, type: ToastType = "success", action?: ToastAction) => {
			const id = nextId++;
			setToasts((prev) => [
				...prev.filter((t) => t.message !== message),
				{ id, message, type, action },
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
				fuse,
				setFuse,
			}}
		>
			{children}
			{/* Portal to <body> at a z-index above modals (z-50/z-60). Toasts open
			    from pages and modals whose glassmorphism backdrop-filter would
			    otherwise sample and blur the toast (it would render behind the
			    modal card as a colored blob), and a filtered ancestor would also
			    trap the fixed-position container. Same fix Modal uses. */}
			{createPortal(
				// The live region is THIS container, not the individual toast, and it
				// is mounted for the life of the app whether or not a toast exists.
				// That is the part that makes announcements work: a live region has to
				// be in the accessibility tree BEFORE its content changes, so marking
				// up each toast as it appears announces nothing reliably. Before this
				// the toasts were never announced at all — they were buttons that
				// silently appeared, which a screen reader has no reason to read out.
				//
				// Polite rather than assertive, including for errors: a toast reports
				// something that already happened, and interrupting whatever the user
				// is reading to say so is worse than waiting for the next pause.
				// aria-atomic="false" so adding a second toast announces only the new
				// one instead of re-reading the whole stack.
				//
				// A real <ol>, so the stack is countable — "list, 3 items", steppable
				// — rather than three unrelated blocks of text arriving from nowhere.
				// It also lets each toast be an <li> instead of a div wearing a role
				// attribute to look like one.
				<ol
					aria-live="polite"
					aria-atomic="false"
					className={`${containerClass} z-[70] m-0 flex list-none flex-col ${alignClass} gap-2 p-0`}
				>
					{toasts.map((t) => (
						<ToastItem
							key={t.id}
							toast={t}
							timeout={timeout}
							fuse={fuse}
							onDone={() => removeToast(t.id)}
						/>
					))}
				</ol>,
				document.body,
			)}
		</ToastContext.Provider>
	);
}

function ToastItem({
	toast,
	timeout,
	fuse,
	onDone,
}: {
	toast: Toast;
	timeout: number;
	fuse: boolean;
	onDone: () => void;
}) {
	const { copy } = useCopyToClipboard({ trackCopied: false });
	const [paused, setPaused] = useState(false);
	const [fading, setFading] = useState(false);
	const startTimeRef = useRef(0);
	const remainingRef = useRef(timeout);
	const timerRef = useRef<ReturnType<typeof setTimeout>>(undefined);

	// Initialize start time on mount (Date.now() is impure during render)
	useEffect(() => {
		if (startTimeRef.current === 0) {
			startTimeRef.current = Date.now();
		}
	}, []);

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

	// Two independent things hold the clock: the pointer being over the toast,
	// and focus being inside it. They overlap constantly — clicking Copy with a
	// mouse both hovers and focuses — so the pause is refcounted rather than a
	// single flag, and the arithmetic runs only on the transitions.
	//
	// Pausing has to be idempotent because startTimeRef is only reset when the
	// timer restarts: subtracting the elapsed span a second time would zero the
	// remaining time, and the toast would vanish the moment the pointer left
	// with seconds still on the clock. Resuming has to check the other holder,
	// or un-hovering would restart the clock under a keyboard user still
	// focused on Dismiss — the exact thing the focus pause exists to prevent.
	const hoverRef = useRef(false);
	const focusRef = useRef(false);
	const pausedRef = useRef(false);

	const pause = () => {
		if (pausedRef.current) return;
		pausedRef.current = true;
		setPaused(true);
		clearTimeout(timerRef.current);
		const elapsed = Date.now() - startTimeRef.current;
		remainingRef.current = Math.max(0, remainingRef.current - elapsed);
	};

	const resume = () => {
		if (!pausedRef.current || hoverRef.current || focusRef.current) return;
		pausedRef.current = false;
		setPaused(false);
		startTimer(remainingRef.current);
	};

	const handleMouseEnter = () => {
		hoverRef.current = true;
		pause();
	};

	const handleMouseLeave = () => {
		hoverRef.current = false;
		resume();
	};

	const handleFocus = () => {
		focusRef.current = true;
		pause();
	};

	const handleBlur = (e: React.FocusEvent<HTMLLIElement>) => {
		// A move between the toast's own buttons is not a departure.
		if (e.currentTarget.contains(e.relatedTarget as Node | null)) return;
		focusRef.current = false;
		resume();
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

	// A blocked clipboard is silent here: a failure toast about a failed copy of
	// a toast would stack on the very message the user is trying to keep.
	const handleCopy = () => {
		void copy(toast.message);
	};

	const { t } = useTranslation();

	return (
		// A toast is a message, not a control, so the body is a plain <li> in the
		// live-region list above, and every action on it is its own real <button>.
		//
		// It used to be one big <button>: clicking anywhere dismissed it, and for
		// errors also copied the message. That made the whole content model invalid
		// the moment anything interactive went inside it — the reason the action
		// slot below had to fake itself with role="button" on a <span>, which
		// screen readers expose unreliably. It also put every visible toast in the
		// tab order announced as "button" with the whole message as its name, and a
		// stack of four toasts meant four tab stops in front of the page.
		//
		// The tradeoff, stated plainly: clicking the body no longer dismisses. The
		// dismiss button is always there, errors get an explicit copy button
		// instead of a hidden click-to-copy discoverable only through a tooltip,
		// and the auto-dismiss timer is unchanged.
		<li
			data-testid="toast"
			data-toast-type={toast.type}
			onMouseEnter={handleMouseEnter}
			onMouseLeave={handleMouseLeave}
			// Focus pauses the timer for the same reason hover does, and matters
			// more: a keyboard user tabbing to Dismiss would otherwise have the
			// button deleted out from under them mid-reach. Capture-phase because
			// focus lands on the buttons inside, not on this element. See the
			// refcount above for why the two holders cannot share one flag.
			//
			// The relatedTarget check in handleBlur is belt-and-braces rather than
			// load-bearing: blur and the following focus are adjacent, so the
			// resume it prevents would be undone before the timer could advance.
			// Kept because the alternative is a resume that only happens to be
			// harmless.
			onFocusCapture={handleFocus}
			onBlurCapture={handleBlur}
			className={`relative flex items-start gap-2 px-4 py-2 rounded-(--radius-card) shadow-lg text-sm font-medium whitespace-pre-line break-words max-w-[min(28rem,90vw)] text-left ${bgColors[toast.type]} ${fading ? "opacity-0" : "opacity-100"}`}
			style={{
				overflow: "hidden",
				transition: "opacity 300ms ease",
			}}
			onTransitionEnd={
				fading
					? (e: React.TransitionEvent) => {
							if (e.propertyName === "opacity") handleAnimationEnd();
						}
					: undefined
			}
		>
			<span data-testid="toast-message" className="min-w-0 flex-1">
				{toast.message}
			</span>
			{toast.action ? (
				<button
					type="button"
					data-testid="toast-action"
					onClick={() => {
						toast.action?.onClick();
						onDone();
					}}
					className="ui-btn ui-btn-ghost ui-btn-compact shrink-0 underline"
				>
					{toast.action.label}
				</button>
			) : null}
			{toast.type === "error" ? (
				// Copy does NOT dismiss. It used to, because it was the same click;
				// separating them means an operator can copy a stack trace and still
				// read the toast it came from.
				<button
					type="button"
					data-testid="toast-copy"
					onClick={handleCopy}
					aria-label={t("common.copy")}
					title={t("common.copy")}
					className="ui-icon-btn shrink-0"
				>
					<Copy className="h-4 w-4" aria-hidden="true" />
				</button>
			) : null}
			<button
				type="button"
				data-testid="toast-dismiss"
				onClick={onDone}
				aria-label={t("common.close")}
				title={t("common.close")}
				className="ui-icon-btn shrink-0"
			>
				<X className="h-4 w-4" aria-hidden="true" />
			</button>
			{fuse && (
				<FuseOutline
					data-testid="toast-fuse"
					color={strokeColors[toast.type]}
					durationMs={timeout}
					paused={paused}
				/>
			)}
		</li>
	);
}

// eslint-disable-next-line react-refresh/only-export-components
export function useToast() {
	return useContext(ToastContext);
}
