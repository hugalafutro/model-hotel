import {
	forwardRef,
	type ReactNode,
	useCallback,
	useEffect,
	useId,
	useImperativeHandle,
	useLayoutEffect,
	useRef,
	useState,
} from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { X } from "@/lib/icons";

export interface ModalHandle {
	close: () => void;
}

interface ModalProps {
	title?: string;
	header?: ReactNode;
	closeOnBackdrop?: boolean;
	onClose: () => void;
	maxWidth?: string;
	scrollable?: boolean;
	children: ReactNode;
	zIndex?: string;
}

const FADE_DURATION = 200;

export const Modal = forwardRef<ModalHandle, ModalProps>(function Modal(
	{
		title,
		header,
		closeOnBackdrop = true,
		onClose,
		maxWidth = "max-w-md",
		scrollable = false,
		children,
		zIndex = "z-50",
	}: ModalProps,
	ref,
) {
	const { t } = useTranslation();
	const dialogRef = useRef<HTMLDivElement>(null);
	const headingId = useId();

	// Fade animation: start invisible, transition to visible after mount.
	// On close: transition back to invisible, then call parent's onClose.
	const [opacity, setOpacity] = useState(0);
	const closingRef = useRef(false);
	const fallbackTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

	// Clear fallback timer on unmount so it doesn't fire on a removed component.
	useEffect(() => {
		return () => {
			if (fallbackTimerRef.current !== null)
				clearTimeout(fallbackTimerRef.current);
		};
	}, []);

	// Fade in on mount (useLayoutEffect + rAF ensures the browser paints
	// opacity-0 first, then transitions to opacity-1).
	useLayoutEffect(() => {
		const id = requestAnimationFrame(() => setOpacity(1));
		return () => cancelAnimationFrame(id);
	}, []);

	// Focus the dialog for keyboard accessibility after fade-in starts
	useEffect(() => {
		dialogRef.current?.focus();
	}, []);

	const handleClose = useCallback(() => {
		if (closingRef.current) return;
		closingRef.current = true;
		setOpacity(0);
		// Fallback: if onTransitionEnd never fires (e.g. jsdom),
		// call onClose after the fade duration so tests don't hang.
		fallbackTimerRef.current = setTimeout(() => {
			if (closingRef.current) onClose();
		}, FADE_DURATION + 50);
	}, [onClose]);

	const handleTransitionEnd = useCallback(
		(e: React.TransitionEvent) => {
			// Only act on the outer wrapper's own opacity transition
			if (e.target !== dialogRef.current || e.propertyName !== "opacity")
				return;
			if (closingRef.current) {
				// Cancel the fallback timer so it cannot fire a second onClose().
				// Keep closingRef.current = true so handleClose() cannot re-enter.
				if (fallbackTimerRef.current !== null) {
					clearTimeout(fallbackTimerRef.current);
					fallbackTimerRef.current = null;
				}
				onClose();
			}
		},
		[onClose],
	);

	const handleKeyDown = useCallback(
		(e: React.KeyboardEvent) => {
			if (e.key === "Escape") handleClose();
		},
		[handleClose],
	);

	useImperativeHandle(ref, () => ({ close: handleClose }), [handleClose]);

	// Portal to <body>: pages open modals from inside glassmorphism cards whose
	// backdrop-filter would otherwise trap the overlay's blur (it could only
	// sample the card, not the page) and hijack position:fixed (a filtered
	// ancestor becomes the containing block for fixed descendants).
	return createPortal(
		<div
			ref={dialogRef}
			role="dialog"
			aria-modal="true"
			aria-labelledby={title || header ? headingId : undefined}
			tabIndex={-1}
			className={`fixed inset-0 flex items-center justify-center ${zIndex} outline-none`}
			style={{
				opacity,
				transition: `opacity ${FADE_DURATION}ms ease`,
			}}
			onKeyDown={handleKeyDown}
			onTransitionEnd={handleTransitionEnd}
		>
			<button
				type="button"
				className="ui-modal-backdrop absolute inset-0 bg-black/60 cursor-default"
				onClick={closeOnBackdrop ? handleClose : undefined}
				aria-label={t("common.closeDialog")}
			/>
			{/* biome-ignore lint/a11y/noStaticElementInteractions: stopPropagation prevents backdrop click bubbling */}
			{/* biome-ignore lint/a11y/useKeyWithClickEvents: purely structural click propagation control */}
			<div
				className={`relative ui-card p-6 w-full ${maxWidth}${
					scrollable ? " max-h-[85vh] flex flex-col" : ""
				}`}
				onClick={(e) => e.stopPropagation()}
			>
				<button
					type="button"
					onClick={handleClose}
					className="ui-icon-btn absolute top-3 right-3 z-10 p-2"
					aria-label={t("common.close")}
				>
					<X size={20} />
				</button>
				{header ? (
					<div id={headingId} className="shrink-0 pr-10">
						{header}
					</div>
				) : (
					title && (
						<h2
							id={headingId}
							className="shrink-0 text-xl font-bold text-white mb-4 pr-10"
						>
							{title}
						</h2>
					)
				)}
				{scrollable ? (
					// pr-2 insets the content from the right edge so the scrollbar
					// can't draw over full-width content (chevrons, divider rules).
					// No negative margin: .ui-card clips to its rounded shape
					// (clip-path) in some themes, which would eat a bled-out gutter.
					<div className="min-h-0 overflow-y-auto pr-2">{children}</div>
				) : (
					children
				)}
			</div>
		</div>,
		document.body,
	);
});
