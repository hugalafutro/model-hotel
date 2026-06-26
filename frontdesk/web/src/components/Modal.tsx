import { type ReactNode, useEffect, useRef } from "react";

interface ModalProps {
	title: string;
	onClose: () => void;
	children: ReactNode;
	actions?: ReactNode;
	// When false, Escape and backdrop click do not dismiss the dialog. Used while
	// a confirmed action is in flight, so the operator cannot accidentally hide
	// the in-progress feedback (the work keeps running server-side regardless).
	// Defaults to true: every existing caller stays dismissible.
	dismissible?: boolean;
}

const FOCUSABLE =
	'a[href],button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])';

// Lightweight modal: backdrop click and Escape close it. The control plane has
// only a handful of dialogs, so this stays deliberately minimal (no portal, no
// focus-trap library), but it does trap Tab focus within the dialog while open
// and restores focus to the trigger on close, so keyboard and screen-reader
// users aren't dropped behind the modal.
export function Modal({
	title,
	onClose,
	children,
	actions,
	dismissible = true,
}: ModalProps) {
	const dialogRef = useRef<HTMLDivElement>(null);

	// Hold onClose in a ref so the focus effect can run exactly once (open + trap
	// + restore-on-close). Callers pass an inline arrow, so depending on onClose
	// directly would re-run the effect on every parent re-render (e.g. toggling an
	// in-modal checkbox), tearing focus back to the first control mid-interaction.
	const onCloseRef = useRef(onClose);
	useEffect(() => {
		onCloseRef.current = onClose;
	}, [onClose]);

	// Same reasoning for dismissible: the keydown listener is registered once, so
	// it must read the current value through a ref rather than the value captured
	// at mount (a dialog opens dismissible, then becomes non-dismissible once its
	// action starts running).
	const dismissibleRef = useRef(dismissible);
	useEffect(() => {
		dismissibleRef.current = dismissible;
	}, [dismissible]);

	useEffect(() => {
		const previouslyFocused = document.activeElement as HTMLElement | null;
		// Focus the first focusable control (or the dialog itself) on open.
		const dialog = dialogRef.current;
		const first = dialog?.querySelector<HTMLElement>(FOCUSABLE);
		(first ?? dialog)?.focus();

		const onKey = (e: KeyboardEvent) => {
			if (e.key === "Escape") {
				if (dismissibleRef.current) onCloseRef.current();
				return;
			}
			if (e.key !== "Tab" || !dialog) return;
			const items = Array.from(dialog.querySelectorAll<HTMLElement>(FOCUSABLE));
			if (items.length === 0) {
				e.preventDefault();
				return;
			}
			const firstItem = items[0];
			const lastItem = items[items.length - 1];
			const active = document.activeElement;
			if (e.shiftKey && active === firstItem) {
				e.preventDefault();
				lastItem.focus();
			} else if (!e.shiftKey && active === lastItem) {
				e.preventDefault();
				firstItem.focus();
			}
		};
		document.addEventListener("keydown", onKey);
		return () => {
			document.removeEventListener("keydown", onKey);
			previouslyFocused?.focus?.();
		};
	}, []);

	return (
		// biome-ignore lint/a11y/noStaticElementInteractions: backdrop click-to-dismiss is a convenience; Escape and the explicit close button are the keyboard-accessible paths.
		<div
			className="fd-modal-backdrop"
			onMouseDown={(e) => {
				if (dismissible && e.target === e.currentTarget) onClose();
			}}
		>
			<div
				ref={dialogRef}
				className="fd-modal"
				role="dialog"
				aria-modal="true"
				aria-label={title}
				tabIndex={-1}
			>
				<h2>{title}</h2>
				<div>{children}</div>
				{actions && <div className="fd-modal-actions">{actions}</div>}
			</div>
		</div>
	);
}
