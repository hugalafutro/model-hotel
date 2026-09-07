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
 * "code" counts as text: OpenRouter reports it for coder models, which
 * serve chat like any text model (mirrors isOpenRouterChatModel).
 */
export function isChatModel(m: {
	modality?: string;
	output_modalities?: string;
}): boolean {
	if (NON_CHAT_MODALITIES.has((m.modality ?? "").toLowerCase())) return false;
	const output = parseModalityArray(m.output_modalities);
	if (output.length > 0 && !output.includes("text") && !output.includes("code"))
		return false;
	return true;
}

/**
 * Non-text output modalities (image/audio/video/embedding/rerank), used to
 * render "produces X" pills alongside the input-capability pills. "code" is
 * text-equivalent (OpenRouter coder models), not a media output.
 */
export function nonTextOutputs(m: { output_modalities?: string }): string[] {
	return parseModalityArray(m.output_modalities).filter(
		(v) => v !== "text" && v !== "code",
	);
}

// A price to at most four decimals. Number#toString already prints the
// shortest round-trip form, so the rounded value never carries trailing zeros
// to trim.
export function formatPrice(n: number | null | undefined): string {
	if (n == null) return "-";
	return String(Math.round(n * 10000) / 10000);
}

/** The same price for a text input, where absent reads as an empty field. */
export function formatPriceInput(n: number | null | undefined): string {
	return n == null ? "" : formatPrice(n);
}

/**
 * Check if an error string indicates a 5XX server error.
 * Matches patterns like "Chat failed: 500 ..." or "Arena failed: 502 ..."
 * where the status code is in the 500-599 range.
 */
export function is5xxError(error: string | null | undefined): boolean {
	return !!error && /\b5\d{2}\b/.test(error);
}

/**
 * The bare model id, without the provider prefix a proxy model id carries
 * ("OpenAI/gpt-4o" reads as "gpt-4o"). An id with no prefix is returned as it is.
 */
export function shortModelName(id: string): string {
	return id.slice(id.lastIndexOf("/") + 1);
}

/** The model a proxy model id names, or undefined when nothing matches. */
export function findChatModel<
	T extends { provider_name: string; model_id: string },
>(models: readonly T[], proxyId: string): T | undefined {
	return models.find(
		(m) => proxyModelID(m.provider_name, m.model_id) === proxyId,
	);
}

/** True when the model a proxy model id names advertises the reasoning capability. */
export function isReasoningModel(
	models: readonly {
		provider_name: string;
		model_id: string;
		capabilities: string;
	}[],
	proxyId: string,
): boolean {
	const model = findChatModel(models, proxyId);
	return model
		? parseCapabilities(model.capabilities).reasoning === true
		: false;
}

/** The proxy model ids of a model list, for membership tests. */
export function chatModelIdSet(
	models: readonly { provider_name: string; model_id: string }[],
): Set<string> {
	return new Set(models.map((m) => proxyModelID(m.provider_name, m.model_id)));
}

/**
 * The model-picker search predicate: a case-insensitive substring of the
 * display name, the model id or the provider name. An empty query matches
 * everything.
 */
export function matchesModelSearch(
	m: { display_name?: string; model_id: string; provider_name: string },
	query: string,
): boolean {
	const q = query.trim().toLowerCase();
	if (q === "") return true;
	return (
		(m.display_name || m.model_id).toLowerCase().includes(q) ||
		m.model_id.toLowerCase().includes(q) ||
		m.provider_name.toLowerCase().includes(q)
	);
}
