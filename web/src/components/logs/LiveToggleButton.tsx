import { useTranslation } from "react-i18next";
import { useToast } from "../../context/ToastContext";

interface LiveToggleButtonProps {
	enabled: boolean;
	onToggle: (next: boolean) => void;
}

export function LiveToggleButton({ enabled, onToggle }: LiveToggleButtonProps) {
	const { t } = useTranslation();
	const { toast } = useToast();

	return (
		<button
			type="button"
			onClick={() => {
				onToggle(!enabled);
				toast(
					enabled
						? t("components.logs.liveToggle.paused")
						: t("components.logs.liveToggle.resumed"),
					"info",
				);
			}}
			className={`ui-badge flex items-center px-1.5 py-px leading-[1.6] text-[10px] font-semibold transition-colors ${
				enabled
					? "ui-badge-success hover:brightness-125"
					: "ui-badge-neutral hover:brightness-125"
			}`}
		>
			<span className="badge-text">{t("components.logs.liveToggle.live")}</span>
		</button>
	);
}
