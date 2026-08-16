// ntfyAppriseURL composes the Apprise target URL for an ntfy topic: https
// servers map to ntfys://host[:port]/topic, plain http to ntfy://. Returns ""
// while the inputs don't form a valid pair yet. Used by the Alerts panel's
// phone-push convenience block (Bellhop plan section 4.3).
export function ntfyAppriseURL(server: string, topic: string): string {
	const cleanTopic = topic.trim();
	if (!cleanTopic || /[\s/]/.test(cleanTopic)) return "";
	let u: URL;
	try {
		u = new URL(server.trim());
	} catch {
		return "";
	}
	if (u.protocol !== "https:" && u.protocol !== "http:") return "";
	const scheme = u.protocol === "https:" ? "ntfys" : "ntfy";
	return `${scheme}://${u.host}/${cleanTopic}`;
}

const TOPIC_ALPHABET =
	"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789";

// generateTopic makes a 20-character secret topic for a fresh ntfy destination so
// the operator never has to invent one. Rejection sampling keeps every character
// equally likely (62 does not divide 256).
export function generateTopic(): string {
	const out: string[] = [];
	const buf = new Uint8Array(64);
	while (out.length < 20) {
		crypto.getRandomValues(buf);
		for (const b of buf) {
			if (b < 248 && out.length < 20) out.push(TOPIC_ALPHABET[b % 62]);
		}
	}
	return out.join("");
}
