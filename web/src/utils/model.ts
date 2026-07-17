export function normalizeProviderName(name: string): string {
	return name.replace(/ /g, "-");
}

export function proxyModelID(providerName: string, modelId: string): string {
	return `${normalizeProviderName(providerName)}/${modelId}`;
}

/**
 * Extract the provider name from a proxy model ID (e.g. "OpenAI/gpt-4o" → "OpenAI").
 * Matches the longest known provider prefix first to avoid false splits on
 * model IDs that may contain slashes.
 */
export function providerFromModelID(
	proxyModelId: string,
	knownProviders: string[] = [],
): string {
	// Sort by descending length so longer (more specific) provider names match first
	const sorted = [...knownProviders].sort((a, b) => b.length - a.length);
	for (const provider of sorted) {
		const normalised = normalizeProviderName(provider);
		if (proxyModelId.startsWith(`${normalised}/`)) {
			return provider;
		}
	}
	// Fallback: take everything before the first slash
	const slashIdx = proxyModelId.indexOf("/");
	return slashIdx > 0 ? proxyModelId.slice(0, slashIdx) : proxyModelId;
}

export function parseCapabilities(capStr: string): Record<string, boolean> {
	try {
		return JSON.parse(capStr);
	} catch {
		return {};
	}
}

/**
 * Non-chat endpoint classes. The backend derives `modality` as an endpoint
 * class with a closed vocabulary (chat, embedding, rerank, image, video, tts,
 * stt — see internal/provider/model_class.go); models with one of these
 * classes are hidden from the chat/arena pickers, where they could never
 * work, but stay visible in /v1/models and the failover group editor.
 */
export const NON_CHAT_MODALITIES = new Set([
	"embedding",
	"rerank",
	"image",
	"video",
	"tts",
	"stt",
]);

/** Parse a modalities field (a JSON array string) to a lowercased list. */
function parseModalityArray(raw: string | undefined): string[] {
	if (!raw) return [];
	try {
		const arr = JSON.parse(raw);
		if (Array.isArray(arr)) return arr.map((s) => String(s).toLowerCase());
	} catch {
		// Not a JSON array — treat as unknown (default-allow below).
	}
	return [];
}

/**
 * True when a model can serve /v1/chat/completions. Default-allow: unknown or
 * empty modalities are treated as chat so a new modality never silently
 * disappears from the picker.
 *
 * Two exclusions: a non-chat endpoint class, or an output that is non-text
 * media only. The latter is defense in depth for rows that predate the
 * class derivation — a model that cannot emit text can never serve chat.
 */
export function isChatModel(m: {
	modality?: string;
	output_modalities?: string;
}): boolean {
	if (NON_CHAT_MODALITIES.has((m.modality ?? "").toLowerCase())) return false;
	const output = parseModalityArray(m.output_modalities);
	if (output.length > 0 && !output.includes("text")) return false;
	return true;
}

/**
 * Non-text output modalities (image/audio/video/embedding/rerank), used to
 * render "produces X" pills alongside the input-capability pills.
 */
export function nonTextOutputs(m: { output_modalities?: string }): string[] {
	return parseModalityArray(m.output_modalities).filter((v) => v !== "text");
}

export function formatPrice(n: number | null | undefined): string {
	if (n == null) return "-";
	const rounded = Math.round(n * 10000) / 10000;
	const str = rounded.toString();
	const [intPart, decPart] = str.split(".");
	if (!decPart) return intPart;
	const trimmed = decPart.replace(/0+$/, "");
	return trimmed.length > 0 ? `${intPart}.${trimmed}` : intPart;
}

export function formatPriceInput(n: number | null | undefined): string {
	if (n == null) return "";
	const rounded = Math.round(n * 10000) / 10000;
	const str = rounded.toString();
	const [intPart, decPart] = str.split(".");
	if (!decPart) return intPart;
	const trimmed = decPart.replace(/0+$/, "");
	return trimmed.length > 0 ? `${intPart}.${trimmed}` : intPart;
}

/**
 * Check if an error string indicates a 5XX server error.
 * Matches patterns like "Chat failed: 500 ..." or "Arena failed: 502 ..."
 * where the status code is in the 500-599 range.
 */
export function is5xxError(error: string | null | undefined): boolean {
	if (!error) return false;
	const match = error.match(/\b(5\d{2})\b/);
	if (!match) return false;
	const code = Number.parseInt(match[1], 10);
	return code >= 500 && code <= 599;
}
