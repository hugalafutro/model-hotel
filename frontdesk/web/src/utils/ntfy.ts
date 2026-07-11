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
