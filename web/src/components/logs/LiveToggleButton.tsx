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
			className={`flex items-center px-1.5 py-px leading-[1.6] rounded text-[10px] font-semibold transition-colors ${
				enabled
					? "bg-green-500/20 text-green-400 hover:bg-green-500/30"
					: "bg-gray-700 text-gray-400 hover:bg-gray-600"
			}`}
		>
			<span className="badge-text">{t("components.logs.liveToggle.live")}</span>
		</button>
	);
}
