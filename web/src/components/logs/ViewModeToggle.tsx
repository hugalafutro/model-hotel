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
			className={`ui-btn ${
				viewMode === "scroll"
					? "ui-btn-primary"
					: "text-gray-400 hover:text-white hover:border-gray-500"
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
