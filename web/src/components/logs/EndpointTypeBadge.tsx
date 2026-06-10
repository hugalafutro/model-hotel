import type { LucideIcon } from "lucide-react";
import { Braces, Image, Mic, Volume2 } from "lucide-react";
import { useTranslation } from "react-i18next";

const ENDPOINT_META: Record<string, { labelKey: string; Icon: LucideIcon }> = {
	embeddings: { labelKey: "logs.endpoint.embeddings", Icon: Braces },
	image: { labelKey: "logs.endpoint.image", Icon: Image },
	tts: { labelKey: "logs.endpoint.tts", Icon: Volume2 },
	stt: { labelKey: "logs.endpoint.stt", Icon: Mic },
};

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
	const meta = ENDPOINT_META[endpointType];
	const label = meta ? t(meta.labelKey) : endpointType;

	if (showLabel) {
		return (
			<span
				data-testid="endpoint-type-badge"
				className="inline-flex items-center gap-1 px-2 py-1 leading-[1.6] rounded-full text-xs font-medium bg-sky-500/15 text-sky-400 border border-sky-500/30"
			>
				{meta && <meta.Icon size={12} />}
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
			{meta ? <meta.Icon size={12} /> : <Braces size={12} />}
		</span>
	);
}
