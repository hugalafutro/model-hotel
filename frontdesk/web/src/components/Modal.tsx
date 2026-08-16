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
	// When false, a backdrop click is ignored; Escape still closes (subject to
	// dismissible and the topmost-dialog rule above). Used by dialogs with
	// meaningful in-progress input (e.g. a multi-step wizard), where a stray
	// click outside the dialog should not discard it. Defaults to true: every
	// existing caller keeps click-to-dismiss.
	closeOnBackdrop?: boolean;
	// Optional secondary line under the title (e.g. a plan name or status).
	subtitle?: ReactNode;
	// Optional controls rendered on the title row, opposite the heading. Callers
	// that omit it keep the previous single-heading layout and focus order.
	headerActions?: ReactNode;
}

const FOCUSABLE =
	'a[href],button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])';

// Every open dialog. Escape is a document listener, so without this each open
// modal answers the same keypress and one press closes a dialog together with
// the one that opened it (e.g. the remove confirmation inside the alerts
// wizard). Only the topmost acts.
const openModals: HTMLElement[] = [];

// The topmost dialog is the one every other open dialog precedes in document
// order, which with no portal and a single shared z-index is also the one
// painted on top: a nested dialog sits inside its opener, a separately opened
// one is appended after it.
function isTopmost(el: HTMLElement): boolean {
	return openModals.every(
		(other) =>
			other === el ||
			(el.compareDocumentPosition(other) & Node.DOCUMENT_POSITION_PRECEDING) !==
				0,
	);
}

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
	closeOnBackdrop = true,
	subtitle,
	headerActions,
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
		if (dialog) openModals.push(dialog);
		const first = dialog?.querySelector<HTMLElement>(FOCUSABLE);
		(first ?? dialog)?.focus();

		const onKey = (e: KeyboardEvent) => {
			if (e.key === "Escape") {
				// Only the topmost dialog answers; the ones underneath stay open.
				if (dialog && !isTopmost(dialog)) return;
				if (dismissibleRef.current) onCloseRef.current();
				return;
			}
			if (e.key !== "Tab" || !dialog) return;
			// Same reasoning as Escape: a dialog underneath must not manage the
			// focus of the one on top, or a wrap inside the topmost lands on a
			// control behind it.
			if (!isTopmost(dialog)) return;
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
			const at = dialog ? openModals.indexOf(dialog) : -1;
			if (at !== -1) openModals.splice(at, 1);
			previouslyFocused?.focus?.();
		};
	}, []);

	return (
		// biome-ignore lint/a11y/noStaticElementInteractions: backdrop click-to-dismiss is a convenience; Escape and the explicit close button are the keyboard-accessible paths.
		<div
			className="fd-modal-backdrop"
			onMouseDown={(e) => {
				if (dismissible && closeOnBackdrop && e.target === e.currentTarget)
					onClose();
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
				<div className="fd-modal-head">
					<div className="fd-modal-heading">
						<h2>{title}</h2>
						{subtitle && <p className="fd-modal-subtitle">{subtitle}</p>}
					</div>
					{headerActions && (
						<div className="fd-modal-head-actions">{headerActions}</div>
					)}
				</div>
				<div>{children}</div>
				{actions && <div className="fd-modal-actions">{actions}</div>}
			</div>
		</div>
	);
}
