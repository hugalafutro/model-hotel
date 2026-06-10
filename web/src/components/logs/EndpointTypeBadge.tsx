import { useTranslation } from "react-i18next";

const ENDPOINT_LABEL_KEYS: Record<string, string> = {
	chat: "logs.endpoint.chat",
	embeddings: "logs.endpoint.embeddings",
	image: "logs.endpoint.image",
	tts: "logs.endpoint.tts",
	stt: "logs.endpoint.stt",
};

/**
 * Small badge marking which endpoint family a request log entry came
 * through. Chat is the default and renders nothing (chat rows stay
 * visually unchanged); multimodal rows get the badge so binary/SSE
 * requests with zero tokens or TPS are self-explanatory.
 */
export function EndpointTypeBadge({ endpointType }: { endpointType?: string }) {
	const { t } = useTranslation();
	if (!endpointType || endpointType === "chat") return null;
	const labelKey = ENDPOINT_LABEL_KEYS[endpointType];
	return (
		<span
			data-testid="endpoint-type-badge"
			className="ml-1 inline-flex items-center px-1.5 rounded text-[10px] font-medium uppercase tracking-wide bg-sky-500/15 text-sky-400 border border-sky-500/30 align-middle"
		>
			{labelKey ? t(labelKey) : endpointType}
		</span>
	);
}
