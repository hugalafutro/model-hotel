import { ntfyAppriseURL } from "@web-shared/ntfy";

// The destination kinds the Alerts setup wizard offers. Every one of them ends
// up as a single Apprise target URL; the wizard only ever asks for the parts an
// operator can copy out of the service's own UI, and this module turns those
// parts into the URL (and back, for the readable list of saved targets).
// Official Apprise docs listing every supported service and its URL shape, the
// same reference the main dashboard links from its alert settings.
export const APPRISE_SERVICES_URL = "https://AppriseIt.com/services/";

export type DestinationKind =
	| "ntfy"
	| "bellhop"
	| "telegram"
	| "discord"
	| "email"
	| "other";

/** Per-kind field values, keyed by `FIELDS[kind][i].key`. */
export interface DestinationFields {
	[key: string]: string;
}

export interface FieldDef {
	key: string;
	/** Entered through a password-type input while typing; not masked after. */
	secret?: boolean;
	placeholder?: string;
	defaultValue?: string;
}

export const FIELDS: Record<DestinationKind, FieldDef[]> = {
	ntfy: [
		{ key: "server", placeholder: "https://ntfy.example.com" },
		{ key: "topic" },
	],
	bellhop: [{ key: "endpoint" }],
	telegram: [{ key: "token", secret: true }, { key: "chat_id" }],
	discord: [{ key: "webhook" }],
	email: [
		{ key: "host" },
		{ key: "port", defaultValue: "587" },
		{ key: "user" },
		{ key: "password", secret: true },
		{ key: "from" },
		{ key: "to" },
	],
	other: [{ key: "url" }],
};

// A UnifiedPush endpoint is what Bellhop shows after it registers with an ntfy
// server: the server origin plus exactly one path segment, the topic. Any query
// (ntfy appends ?up=1) is part of the protocol, not the topic, so it is dropped.
export function parseUnifiedPushEndpoint(
	s: string,
): { server: string; topic: string } | null {
	let u: URL;
	try {
		u = new URL(s.trim());
	} catch {
		return null;
	}
	if (u.protocol !== "https:" && u.protocol !== "http:") return null;
	const segments = u.pathname.split("/").filter(Boolean);
	if (segments.length !== 1) return null;
	return { server: u.origin, topic: segments[0] };
}

const DISCORD_WEBHOOK =
	/^https:\/\/(?:discord|discordapp)\.com\/api(?:\/v\d+)?\/webhooks\/([^/?#]+)\/([^/?#]+)\/?$/i;

// Discord hands out the whole webhook URL, so the wizard takes that verbatim
// and splits out the id/token pair Apprise wants.
export function parseDiscordWebhook(
	s: string,
): { id: string; token: string } | null {
	const m = DISCORD_WEBHOOK.exec(s.trim());
	if (!m) return null;
	return { id: m[1], token: m[2] };
}

// compose turns the fields of one kind into its Apprise target URL, and returns
// "" for anything not yet complete or not yet valid. The wizard calls it on
// every keystroke, so "" is the normal state while the operator is still typing
// rather than an error worth reporting.
export function compose(kind: DestinationKind, f: DestinationFields): string {
	switch (kind) {
		case "ntfy":
			return ntfyAppriseURL(f.server ?? "", f.topic ?? "");
		case "bellhop": {
			const parsed = parseUnifiedPushEndpoint(f.endpoint ?? "");
			if (!parsed) return "";
			return ntfyAppriseURL(parsed.server, parsed.topic);
		}
		case "telegram": {
			const token = (f.token ?? "").trim();
			const chatID = (f.chat_id ?? "").trim();
			if (!token || !chatID) return "";
			if (/[\s/]/.test(token)) return "";
			return `tgram://${token}/${chatID}`;
		}
		case "discord": {
			const parsed = parseDiscordWebhook(f.webhook ?? "");
			if (!parsed) return "";
			return `discord://${parsed.id}/${parsed.token}`;
		}
		case "email": {
			const host = (f.host ?? "").trim();
			const port = (f.port ?? "").trim();
			const user = (f.user ?? "").trim();
			const password = f.password ?? "";
			const to = (f.to ?? "").trim();
			const from = (f.from ?? "").trim() || user;
			if (!host || !user || !password || !to) return "";
			if (!/^\d+$/.test(port)) return "";
			return `mailtos://${encodeURIComponent(user)}:${encodeURIComponent(password)}@${host}:${port}?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`;
		}
		case "other": {
			const url = (f.url ?? "").trim();
			return /^[a-z][a-z0-9+.-]*:\/\/\S+$/i.test(url) ? url : "";
		}
	}
}

export interface TargetInfo {
	kind: DestinationKind;
	host: string;
	/** The identifying segment the destination row highlights: topic, token, password. */
	secret: string;
	url: string;
}

// Bellhop asks its ntfy server for a topic and gets an opaque "up"-prefixed one
// back, which is how a saved ntfy target can be told apart from a topic the
// operator chose by hand. Both are ntfy URLs; only the label differs.
function isBellhopTopic(topic: string): boolean {
	return topic.startsWith("up") && topic.length >= 8;
}

// Percent-decodes a URL component, falling back to the raw text when the value
// was written by hand and holds a stray "%".
function decodePart(s: string): string {
	try {
		return decodeURIComponent(s);
	} catch {
		return s;
	}
}

// describeTarget reads a stored Apprise URL back into something the settings
// list can show: which service it points at, which host, and the identifying
// segment the destination row highlights (topic, token, password). Stored
// targets come from the server, but a hand-edited or future-scheme value must
// still render, so this never throws.
export function describeTarget(url: string): TargetInfo {
	const fallback: TargetInfo = {
		kind: "other",
		host: url.split("://")[0] ?? "",
		secret: "",
		url,
	};
	const sep = url.indexOf("://");
	if (sep <= 0) return fallback;
	const scheme = url.slice(0, sep).toLowerCase();
	const rest = url.slice(sep + 3);
	const segments = rest.split("?")[0].split("#")[0].split("/").filter(Boolean);
	try {
		switch (scheme) {
			case "ntfy":
			case "ntfys": {
				const u = new URL(url);
				const path = u.pathname.split("/").filter(Boolean);
				const topic = path[path.length - 1] ?? "";
				return {
					kind: isBellhopTopic(topic) ? "bellhop" : "ntfy",
					host: u.host,
					secret: topic,
					url,
				};
			}
			case "tgram":
				return {
					kind: "telegram",
					host: "api.telegram.org",
					secret: segments[0] ?? "",
					url,
				};
			case "discord":
				return {
					kind: "discord",
					host: "discord.com",
					secret: segments[1] ?? "",
					url,
				};
			case "mailto":
			case "mailtos": {
				const u = new URL(url);
				return {
					kind: "email",
					host: u.host,
					secret: decodePart(u.password),
					url,
				};
			}
			default:
				return { kind: "other", host: scheme, secret: "", url };
		}
	} catch {
		return fallback;
	}
}

// ntfyServerOf recovers the ntfy server already in use so a second phone can be
// added without asking the operator for the URL again.
export function ntfyServerOf(targets: string[]): string {
	for (const t of targets) {
		const info = describeTarget(t);
		if (info.kind !== "ntfy" && info.kind !== "bellhop") continue;
		const scheme = t.toLowerCase().startsWith("ntfys://") ? "https" : "http";
		return `${scheme}://${info.host}`;
	}
	return "";
}
