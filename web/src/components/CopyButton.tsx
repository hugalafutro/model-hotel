import { useTranslation } from "react-i18next";
import { Copy } from "@/lib/icons";
import { type ToastType, useToast } from "../context/ToastContext";
import { useCopyToClipboard } from "../hooks/useCopyToClipboard";

/**
 * How the button looks and how it reports the result:
 * - "icon": a copy glyph, result announced as a toast.
 * - "label": a Copy/Copied text button that says it by swapping its own label.
 *   A blocked clipboard is silent there; the copied text stays selectable.
 */
type CopyButtonVariant = "icon" | "label";

interface CopyButtonProps {
	text: string;
	size?: number;
	className?: string;
	title?: string;
	/** Toast severity shown on a successful copy. Defaults to "info". Icon variant only. */
	toastType?: ToastType;
	variant?: CopyButtonVariant;
	/** Accessible name; falls back to `title`, then the shared "Copy" label. */
	ariaLabel?: string;
	testId?: string;
}

export function CopyButton({
	text,
	size = 10,
	className,
	title,
	toastType = "info",
	variant = "icon",
	ariaLabel,
	testId,
}: CopyButtonProps) {
	const { t } = useTranslation();
	const { toast } = useToast();
	const { copy, copied } = useCopyToClipboard();
	const isIcon = variant === "icon";

	const handleClick = async () => {
		const ok = await copy(text);
		// The label variant reports itself; only the icon needs the toast.
		if (!isIcon) return;
		if (ok) toast(t("common.copiedToClipboard"), toastType);
		else toast(t("common.failedToCopy"), "error");
	};

	return (
		<button
			type="button"
			className={
				className ??
				(isIcon
					? "ui-icon-btn inline-flex items-center"
					: "ui-btn ui-btn-secondary")
			}
			data-testid={testId}
			onClick={handleClick}
			title={isIcon ? (title ?? t("common.copy")) : title}
			aria-label={ariaLabel ?? title ?? t("common.copy")}
		>
			{isIcon ? (
				<Copy size={size} />
			) : copied ? (
				t("common.copied")
			) : (
				t("common.copy")
			)}
		</button>
	);
}
