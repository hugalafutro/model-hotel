import { useTranslation } from "react-i18next";
import { Copy } from "@/lib/icons";
import { useToast } from "../context/ToastContext";

interface CopyButtonProps {
	text: string;
	size?: number;
	className?: string;
	title?: string;
}

export function CopyButton({
	text,
	size = 10,
	className = "ui-icon-btn inline-flex items-center",
	title,
}: CopyButtonProps) {
	const { t } = useTranslation();
	const { toast } = useToast();
	return (
		<button
			type="button"
			className={className}
			onClick={() => {
				navigator.clipboard
					.writeText(text)
					.then(() => toast(t("common.copiedToClipboard"), "info"))
					.catch(() => toast(t("common.failedToCopy"), "error"));
			}}
			title={title ?? t("common.copy")}
			aria-label={title ?? t("common.copy")}
		>
			<Copy size={size} />
		</button>
	);
}
