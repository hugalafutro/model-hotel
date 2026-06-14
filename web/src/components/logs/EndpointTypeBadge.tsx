import { useTranslation } from "react-i18next";
import { Braces } from "@/lib/icons";
import { ENDPOINT_ICONS, ENDPOINT_LABEL_KEYS } from "./endpointTypes";

/**
 * Compact marker for which endpoint family a request log entry came
 * through. Chat is the default and renders nothing (chat rows stay
 * visually unchanged); multimodal rows get the marker so binary/SSE
 * requests with zero tokens or TPS are self-explanatory.
 *
 * Default form is an icon meant to lead the model cell (leading position
 * survives the cell's `truncate` clipping); the family name is exposed via
 * tooltip and aria-label. `showLabel` renders the full icon+text pill for
 * roomy contexts like the request detail header.
 */
export function EndpointTypeBadge({
	endpointType,
	showLabel = false,
}: {
	endpointType?: string;
	showLabel?: boolean;
}) {
	const { t } = useTranslation();
	if (!endpointType || endpointType === "chat") return null;
	const labelKey = ENDPOINT_LABEL_KEYS[endpointType];
	const label = labelKey ? t(labelKey) : endpointType;
	const Icon = ENDPOINT_ICONS[endpointType] ?? Braces;

	if (showLabel) {
		return (
			<span
				data-testid="endpoint-type-badge"
				className="ui-badge ui-badge-info inline-flex items-center gap-1 px-2 py-1 leading-[1.6] text-xs font-medium"
			>
				<Icon size={12} />
				<span className="badge-text">{label}</span>
			</span>
		);
	}

	return (
		<span
			data-testid="endpoint-type-badge"
			role="img"
			title={label}
			aria-label={label}
			className="mr-1 inline-flex shrink-0 align-middle text-sky-400"
		>
			<Icon size={12} />
		</span>
	);
}
