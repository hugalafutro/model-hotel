export function normalizeProviderName(name: string): string {
    return name.replace(/ /g, "-");
}

export function proxyModelID(providerName: string, modelId: string): string {
    return normalizeProviderName(providerName) + "/" + modelId;
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
        if (proxyModelId.startsWith(normalised + "/")) {
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
