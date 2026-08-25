import { useTranslation } from "react-i18next";
import { InfiniteScroll, Pages } from "@/lib/icons";

export type ViewMode = "paginate" | "scroll";

interface ViewModeToggleProps {
	viewMode: ViewMode;
	onChange: (mode: ViewMode) => void;
}

/** Either-or switch between infinite scroll and pagination, shared by the
 *  Requests, Logs, Models and Audit tables. There is no off state: the button
 *  is always accent-styled and only the glyph changes, showing the mode the
 *  table is IN right now (not the one a click would switch to). The glyph is
 *  decorative, so a visually hidden span names the current mode for assistive
 *  tech; the tooltip sentence rides along as the description via title. */
export function ViewModeToggle({ viewMode, onChange }: ViewModeToggleProps) {
	const { t } = useTranslation();
	const Icon = viewMode === "scroll" ? InfiniteScroll : Pages;

	return (
		<button
			type="button"
			onClick={() => onChange(viewMode === "paginate" ? "scroll" : "paginate")}
			className="ui-btn ui-btn-primary ui-btn-icon"
			title={t("components.viewModeToggle.tooltip")}
		>
			<Icon size={14} aria-hidden="true" />
			<span className="sr-only">
				{t(
					viewMode === "scroll"
						? "components.viewModeToggle.scrolling"
						: "components.viewModeToggle.pagination",
				)}
			</span>
		</button>
	);
}
