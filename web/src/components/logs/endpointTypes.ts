import type { LucideIcon } from "lucide-react";
import { Braces, Image, Mic, Volume2 } from "lucide-react";

// Single source of truth for the endpoint families and their i18n label
// keys on the frontend; the badge and the Logs filter dropdown both derive
// from it. Insertion order is the dropdown display order.
export const ENDPOINT_LABEL_KEYS: Record<string, string> = {
	chat: "logs.endpoint.chat",
	embeddings: "logs.endpoint.embeddings",
	image: "logs.endpoint.image",
	tts: "logs.endpoint.tts",
	stt: "logs.endpoint.stt",
};

export const ENDPOINT_ICONS: Record<string, LucideIcon> = {
	embeddings: Braces,
	image: Image,
	tts: Volume2,
	stt: Mic,
};

/**
 * Ordered options for the Logs endpoint filter dropdown. Labels resolve
 * through the same key table the badge uses, so adding a family is a
 * single-entry change (plus its en.json key and icon).
 */
export const ENDPOINT_FILTER_OPTIONS = Object.entries(ENDPOINT_LABEL_KEYS).map(
	([value, labelKey]) => ({ value, labelKey }),
);
