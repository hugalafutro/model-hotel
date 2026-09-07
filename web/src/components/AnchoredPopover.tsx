import {
	type ReactNode,
	useCallback,
	useLayoutEffect,
	useRef,
	useState,
} from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { X } from "@/lib/icons";
import { useClickOutside } from "../hooks/useClickOutside";

/** w-72 = 18rem. */
const POPOVER_WIDTH = 288;
/** mt-2, between the trigger and the popover. */
const GAP = 8;

interface AnchoredPopoverProps {
	/**
	 * `data-popover-trigger` value of the button this popover hangs from. The
	 * trigger is found by that attribute rather than by aria-label, which can
	 * change while the popover is open.
	 */
	triggerSelector: string;
	/**
	 * Container holding the trigger. When given, the lookup is scoped to it so
	 * several instances on one page do not find each other's trigger.
	 */
	triggerRef?: React.RefObject<HTMLElement | null>;
	/** Which edge of the trigger the popover lines up with. */
	anchor?: "left" | "right";
	title: string;
	onClose: () => void;
	children: ReactNode;
}

/**
 * A popover portalled to the body, so no overflow-hidden ancestor can clip it,
 * positioned under its trigger and tracking it on scroll and resize. Clicking
 * outside closes it; the trigger itself does not count as outside, since it
 * toggles the popover on its own.
 */
export function AnchoredPopover({
	triggerSelector,
	triggerRef,
	anchor = "right",
	title,
	onClose,
	children,
}: AnchoredPopoverProps) {
	const { t } = useTranslation();
	const popoverRef = useRef<HTMLDivElement>(null);
	const [position, setPosition] = useState({ top: 0, left: 0 });
	const query = useCallback(
		() =>
			(triggerRef?.current ?? document).querySelector<HTMLElement>(
				`[data-popover-trigger="${triggerSelector}"]`,
			),
		[triggerRef, triggerSelector],
	);

	useLayoutEffect(() => {
		const trigger = query();
		if (!trigger) return;

		const reposition = () => {
			const rect = trigger.getBoundingClientRect();
			let left = anchor === "right" ? rect.right - POPOVER_WIDTH : rect.left;
			// Clamp to the viewport so the popover never renders off-screen.
			if (left < 0) left = 0;
			if (left + POPOVER_WIDTH > window.innerWidth)
				left = window.innerWidth - POPOVER_WIDTH;
			setPosition({ top: rect.bottom + GAP, left });
		};

		reposition();
		window.addEventListener("scroll", reposition, true);
		window.addEventListener("resize", reposition);
		return () => {
			window.removeEventListener("scroll", reposition, true);
			window.removeEventListener("resize", reposition);
		};
	}, [anchor, query]);

	useClickOutside(popoverRef, onClose, { ignore: query });

	return createPortal(
		<div
			ref={popoverRef}
			className="fixed w-72 p-4 ui-card shadow-2xl z-50"
			style={{ top: position.top, left: position.left }}
		>
			<div className="flex items-center justify-between mb-3">
				<span className="text-sm font-semibold text-(--text-primary)">
					{title}
				</span>
				<button
					type="button"
					onClick={onClose}
					className="ui-icon-btn leading-none p-1"
					title={t("components.logs.dateRangePicker.close")}
					aria-label={t("components.logs.dateRangePicker.close")}
				>
					<X size={16} />
				</button>
			</div>
			{children}
		</div>,
		document.body,
	);
}
