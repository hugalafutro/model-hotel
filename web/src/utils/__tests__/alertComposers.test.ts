import {
	compose,
	describeTarget,
	ntfyServerOf,
	parseDiscordWebhook,
	parseUnifiedPushEndpoint,
} from "@web-shared/alerts/composers";
import { describe, expect, it } from "vitest";

// The alert destination composers, shared with Front Desk. Tested here rather
// than under pages/Settings/alerts: the module backs the alerts UI in both
// frontends and belongs to neither app's page tree.

describe("compose", () => {
	it("composes ntfy from server + topic and stays empty while invalid", () => {
		expect(
			compose("ntfy", { server: "https://ntfy.example.com", topic: "abc" }),
		).toBe("ntfys://ntfy.example.com/abc");
		expect(
			compose("ntfy", { server: "http://ntfy.lan:2586", topic: "abc" }),
		).toBe("ntfy://ntfy.lan:2586/abc");
		expect(
			compose("ntfy", { server: "https://ntfy.example.com", topic: "" }),
		).toBe("");
		expect(compose("ntfy", { server: "ntfy.example.com", topic: "abc" })).toBe(
			"",
		);
	});
	it("composes bellhop from a pasted UnifiedPush endpoint", () => {
		expect(
			compose("bellhop", {
				endpoint: "https://ntfy.example.com/upAbCdEfGh?up=1",
			}),
		).toBe("ntfys://ntfy.example.com/upAbCdEfGh");
		expect(compose("bellhop", { endpoint: "not a url" })).toBe("");
	});
	it("composes telegram, discord, email, other", () => {
		expect(compose("telegram", { token: "123:abc", chat_id: "42" })).toBe(
			"tgram://123:abc/42",
		);
		expect(compose("telegram", { token: "", chat_id: "42" })).toBe("");
		expect(compose("telegram", { token: "a/b", chat_id: "1" })).toBe("");
		expect(
			compose("discord", {
				webhook: "https://discord.com/api/webhooks/111/tok_en",
			}),
		).toBe("discord://111/tok_en");
		expect(
			compose("email", {
				host: "smtp.example.com",
				port: "587",
				user: "me@example.com",
				password: "p w",
				from: "",
				to: "you@example.com",
			}),
		).toBe(
			"mailtos://me%40example.com:p%20w@smtp.example.com:587?from=me%40example.com&to=you%40example.com",
		);
		expect(
			compose("email", {
				host: "smtp.example.com",
				port: "x",
				user: "u",
				password: "p",
				from: "",
				to: "t",
			}),
		).toBe("");
		expect(compose("other", { url: "slack://T/B/C" })).toBe("slack://T/B/C");
		expect(compose("other", { url: "no scheme" })).toBe("");
	});
});

describe("parsers", () => {
	it("parseUnifiedPushEndpoint accepts host/topic with or without ?up=1", () => {
		expect(
			parseUnifiedPushEndpoint("https://ntfy.example.com/upAbCdEfGh?up=1"),
		).toEqual({ server: "https://ntfy.example.com", topic: "upAbCdEfGh" });
		expect(
			parseUnifiedPushEndpoint(" https://ntfy.example.com/upAbCdEfGh "),
		).toEqual({ server: "https://ntfy.example.com", topic: "upAbCdEfGh" });
		expect(parseUnifiedPushEndpoint("https://ntfy.example.com/")).toBeNull();
		expect(parseUnifiedPushEndpoint("https://ntfy.example.com/a/b")).toBeNull();
		expect(parseUnifiedPushEndpoint("ftp://x/y")).toBeNull();
	});
	it("parseDiscordWebhook", () => {
		expect(parseDiscordWebhook("https://discord.com/api/webhooks/1/t")).toEqual(
			{ id: "1", token: "t" },
		);
		expect(
			parseDiscordWebhook("https://discordapp.com/api/v10/webhooks/1/t"),
		).toEqual({ id: "1", token: "t" });
		expect(
			parseDiscordWebhook("https://example.com/api/webhooks/1/t"),
		).toBeNull();
	});
});

describe("describeTarget / ntfyServerOf", () => {
	it("classifies stored targets for the readable list", () => {
		expect(describeTarget("ntfys://ntfy.example.com/secret1")).toMatchObject({
			kind: "ntfy",
			host: "ntfy.example.com",
			secret: "secret1",
		});
		expect(describeTarget("ntfys://ntfy.example.com/upZyXwVuTs")).toMatchObject(
			{ kind: "bellhop", secret: "upZyXwVuTs" },
		);
		expect(describeTarget("tgram://123:abc/42")).toMatchObject({
			kind: "telegram",
			secret: "123:abc",
		});
		expect(describeTarget("discord://111/tok")).toMatchObject({
			kind: "discord",
			secret: "tok",
		});
		expect(
			describeTarget("mailtos://u:p@smtp.example.com:587?to=x"),
		).toMatchObject({ kind: "email", host: "smtp.example.com:587" });
		expect(
			describeTarget("mailtos://u:p%40w@smtp.example.com:587?to=x").secret,
		).toBe("p@w");
		expect(describeTarget("mailtos://u:p%@h/?to=x")).toMatchObject({
			kind: "email",
		});
		expect(describeTarget("slack://T/B/C")).toMatchObject({
			kind: "other",
			host: "slack",
		});
		expect(describeTarget("garbage")).toMatchObject({ kind: "other" });
	});
	it("ntfyServerOf finds the first ntfy server", () => {
		expect(ntfyServerOf(["tgram://a/b", "ntfys://ntfy.example.com/x"])).toBe(
			"https://ntfy.example.com",
		);
		expect(ntfyServerOf(["ntfy://ntfy.lan:2586/x"])).toBe(
			"http://ntfy.lan:2586",
		);
		expect(ntfyServerOf([])).toBe("");
	});
});
