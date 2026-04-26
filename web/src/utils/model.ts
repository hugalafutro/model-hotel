export function proxyModelID(providerName: string, modelId: string): string {
    return providerName.replace(/ /g, "-") + "/" + modelId;
}

export function parseCapabilities(
    capStr: string,
): Record<string, boolean> {
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
