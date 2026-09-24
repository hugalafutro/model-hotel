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
import { ModalNav, type ModalNavProps } from "./ModalNav";

export interface ModalHandle {
	close: () => void;
}

interface ModalProps {
	title?: string;
	header?: ReactNode;
	closeOnBackdrop?: boolean;
	// Whether the quiet exits (Escape, the header X) are offered. False while the
	// dialog is doing work that walking out would strand, e.g. a write in flight:
	// the caller keeps its own explicit buttons and decides when leaving is safe.
	dismissible?: boolean;
	// Prev/next stepper drawn beside the close button, for a dialog opened from
	// one row of a list. Absent for dialogs with no list behind them, and not
	// combined with dismissible={false} by callers: stepping swaps the dialog's
	// subject, which is the thing that flag exists to prevent.
	nav?: ModalNavProps;
	onClose: () => void;
	maxWidth?: string;
	scrollable?: boolean;
	children: ReactNode;
	zIndex?: string;
}

const FADE_DURATION = 200;

/**
 * Every mounted Modal's dialog node, in mount order, so a document-level
 * Escape can be given to the topmost one only.
 *
 * Module scope rather than context: modals portal to <body> from anywhere in
 * the tree, including from siblings that share no provider, and nesting is
 * decided by what is on screen rather than by who rendered whom.
 */
const openDialogs: HTMLElement[] = [];

/**
 * Elements whose own arrow-key behaviour outranks the row stepper: the fields
 * a caret moves through, and the roles that move a selection.
 */
const ARROW_KEY_OWNERS = [
	"input",
	"textarea",
	"select",
	"[contenteditable='true']",
	"[role='listbox']",
	"[role='combobox']",
	"[role='tablist']",
	"[role='menu']",
	"[role='menubar']",
	"[role='grid']",
	"[role='tree']",
	"[role='treegrid']",
	"[role='radiogroup']",
	"[role='slider']",
	"[role='spinbutton']",
	"[role='textbox']",
].join(", ");

/** Width of each max-w-* tier in rem. The dialog keeps this width up to a
 * 1080p screen, then widens with the viewport (see .ui-modal-panel). */
const MODAL_TIER_REM: Record<string, number> = {
	"max-w-sm": 24,
	"max-w-md": 28,
	"max-w-lg": 32,
	"max-w-xl": 36,
	"max-w-2xl": 42,
	"max-w-3xl": 48,
	"max-w-4xl": 56,
	"max-w-5xl": 64,
	"max-w-6xl": 72,
};

/** What Tab can land on inside the dialog. */
const FOCUSABLE_SELECTOR =
	'a[href], button, input, select, textarea, [tabindex]:not([tabindex="-1"])';

export const Modal = forwardRef<ModalHandle, ModalProps>(function Modal(
	{
		title,
		header,
		closeOnBackdrop = true,
		dismissible = true,
		nav,
		onClose,
		maxWidth = "max-w-md",
		scrollable = false,
		children,
		zIndex = "z-50",
	}: ModalProps,
	ref,
) {
	const { t } = useTranslation();
	const tierRem = MODAL_TIER_REM[maxWidth];
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

	// Focus the dialog for keyboard accessibility after fade-in starts, and
	// hand focus back to whatever opened it when it unmounts: aria-modal says
	// the rest of the page is inert, so a keyboard user must not be dropped on
	// <body> and made to start over from the top.
	useEffect(() => {
		const opener = document.activeElement;
		dialogRef.current?.focus();
		return () => {
			if (opener instanceof HTMLElement && opener.isConnected) opener.focus();
		};
	}, []);

	// Tab stays inside the dialog: the page behind the portal is still in the
	// tab order, and a Tab past the last control would otherwise land on the
	// sidebar, where Enter navigates with the dialog still open.
	const handleTabKey = useCallback((e: React.KeyboardEvent<HTMLDivElement>) => {
		// A modified Tab (Ctrl/Cmd+Tab switches browser tabs) is not ours.
		if (e.key !== "Tab" || e.ctrlKey || e.metaKey || e.altKey) return;
		const root = dialogRef.current;
		if (!root) return;
		const focusables = Array.from(
			root.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR),
		).filter((el) => !el.hasAttribute("disabled"));
		if (focusables.length === 0) {
			e.preventDefault();
			return;
		}
		const first = focusables[0];
		const last = focusables[focusables.length - 1];
		const active = document.activeElement;
		if (e.shiftKey && (active === first || active === root)) {
			e.preventDefault();
			last.focus();
		} else if (!e.shiftKey && active === last) {
			e.preventDefault();
			first.focus();
		}
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

	// Read by the document listener below, which must not re-register when
	// `onClose` changes identity: re-registering re-pushes this dialog onto
	// `openDialogs`, and a parent that outranks its own child swallows the
	// Escape meant to close the child.
	const closeRef = useRef(handleClose);
	useEffect(() => {
		closeRef.current = handleClose;
	}, [handleClose]);

	// Same reason as closeRef: the keydown listener registers once, so it reads
	// the current value here rather than closing over the one it mounted with.
	const dismissibleRef = useRef(dismissible);
	useEffect(() => {
		dismissibleRef.current = dismissible;
	}, [dismissible]);

	// Ditto for the stepper: a page rebuilds its callbacks whenever the list
	// behind the dialog is refetched, which is often.
	const navRef = useRef(nav);
	useEffect(() => {
		navRef.current = nav;
	}, [nav]);

	const scrollRef = useRef<HTMLDivElement>(null);
	// Stepping to another row starts that row at the top: the dialog is one
	// scroll container reused for every row, so a long row scrolled to its
	// end would otherwise hand the next row a scroll position it never had.
	// Set before the new row renders, so it never paints at the old offset.
	const stepTo = useCallback((go: () => void) => {
		go();
		if (scrollRef.current) scrollRef.current.scrollTop = 0;
	}, []);

	// Escape and the stepper's arrow keys are handled on the DOCUMENT, not on
	// the dialog node.
	//
	// A control that unmounts while focused — dismissing the row whose button
	// you just clicked — hands focus back to <body>, which is outside this
	// subtree. A dialog-scoped handler never sees the key from there, so the
	// modal silently stops closing on Escape for the rest of its life.
	//
	// Topmost only, so a nested confirm closes before the dialog that opened it.
	// Mount order is the stacking order: modals portal to <body> in the order
	// they open, and the one opened last is the one drawn on top.
	//
	// stepTo is the one dependency, and it holds no props or state, so it
	// never changes identity and the listener still registers exactly once
	// per dialog.
	useEffect(() => {
		const el = dialogRef.current;
		if (!el) return;
		openDialogs.push(el);
		const onKeyDown = (e: KeyboardEvent) => {
			if (openDialogs[openDialogs.length - 1] !== el) return;
			if (e.key === "Escape") {
				if (dismissibleRef.current) closeRef.current();
				return;
			}
			if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
			const nav = navRef.current;
			if (!nav || e.defaultPrevented) return;
			// Alt+Arrow is browser history, and the rest carry their own
			// meanings in a text field or a shortcut.
			if (e.altKey || e.ctrlKey || e.metaKey || e.shiftKey) return;
			// Arrow keys belong to whatever the user is typing in, and to the
			// widgets that move a selection with them.
			const target = e.target as HTMLElement | null;
			if (target?.closest?.(ARROW_KEY_OWNERS)) return;
			const step =
				e.key === "ArrowLeft"
					? nav.index > 0 && nav.onPrev
					: nav.index < nav.total - 1 && nav.onNext;
			if (!step) return;
			// Consumed: the same press must not also scroll the dialog, and a
			// listener further out can see the key was taken.
			e.preventDefault();
			stepTo(step);
		};
		document.addEventListener("keydown", onKeyDown);
		return () => {
			document.removeEventListener("keydown", onKeyDown);
			const i = openDialogs.indexOf(el);
			if (i !== -1) openDialogs.splice(i, 1);
		};
	}, [stepTo]);

	useImperativeHandle(ref, () => ({ close: handleClose }), [handleClose]);

	// Title and header keep clear of the corner controls: the close button
	// alone, or the stepper's two arrows plus the close button.
	const headerPadding = nav ? "pr-32" : "pr-10";
	// A dialog with a stepper hangs from a fixed top edge instead of centring:
	// rows differ in height, and a centred dialog would move its arrows up or
	// down on every step, out from under a pointer clicking through the rows.
	const placement = nav ? "items-start pt-[7.5vh]" : "items-center";

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
			className={`fixed inset-0 flex ${placement} justify-center ${zIndex} outline-none`}
			style={{
				opacity,
				transition: `opacity ${FADE_DURATION}ms ease`,
			}}
			onTransitionEnd={handleTransitionEnd}
			onKeyDown={handleTabKey}
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
					tierRem ? " ui-modal-panel" : ""
				}${scrollable ? " max-h-[85vh] flex flex-col" : ""}`}
				style={
					tierRem
						? ({ "--modal-w": tierRem } as React.CSSProperties)
						: undefined
				}
				onClick={(e) => e.stopPropagation()}
			>
				<div className="absolute top-3 right-3 z-10 flex items-center gap-1">
					{nav && (
						<ModalNav
							index={nav.index}
							total={nav.total}
							onPrev={() => stepTo(nav.onPrev)}
							onNext={() => stepTo(nav.onNext)}
						/>
					)}
					<button
						type="button"
						onClick={handleClose}
						disabled={!dismissible}
						className="ui-icon-btn p-2"
						aria-label={t("common.close")}
					>
						<X size={20} />
					</button>
				</div>
				{header ? (
					<div id={headingId} className={`shrink-0 ${headerPadding}`}>
						{header}
					</div>
				) : (
					title && (
						<h2
							id={headingId}
							className={`shrink-0 ui-modal-title mb-4 ${headerPadding}`}
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
					// data-modal-scroll lets a descendant resolve its own scroll root
					// (element.closest) without Modal growing a ref prop. Used by the
					// discrepancy modal's return-to-top IntersectionObserver.
					<div
						ref={scrollRef}
						className="min-h-0 overflow-y-auto pr-2"
						data-modal-scroll
					>
						{children}
					</div>
				) : (
					children
				)}
			</div>
		</div>,
		document.body,
	);
});
