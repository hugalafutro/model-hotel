import { useTranslation } from "react-i18next";

interface ViewModeToggleProps {
	viewMode: "paginate" | "scroll";
	onChange: (mode: "paginate" | "scroll") => void;
}

export function ViewModeToggle({ viewMode, onChange }: ViewModeToggleProps) {
	const { t } = useTranslation();

	return (
		<button
			type="button"
			onClick={() => onChange(viewMode === "paginate" ? "scroll" : "paginate")}
			className={`flex items-center gap-1 px-2 py-1.5 rounded-md text-xs font-medium transition-all border cursor-pointer ${
				viewMode === "scroll"
					? "bg-(--accent)/20 text-(--accent) border-(--accent)/40"
					: "text-gray-400 border-gray-700 hover:text-(--text-primary) hover:border-gray-500"
			}`}
			title={
				viewMode === "paginate"
					? t("components.logs.viewModeToggle.switchToScroll")
					: t("components.logs.viewModeToggle.switchToPagination")
			}
			aria-label={
				viewMode === "paginate"
					? t("components.logs.viewModeToggle.switchToScroll")
					: t("components.logs.viewModeToggle.switchToPagination")
			}
		>
			{viewMode === "paginate"
				? t("components.logs.viewModeToggle.scroll")
				: t("components.logs.viewModeToggle.pages")}
		</button>
	);
}
