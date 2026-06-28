import { describe, expect, it } from "vitest";
import {
	formatTimestamp,
	getLevelBadgeVariant,
	getSourceBadgeClasses,
} from "./logBadgeUtils";

describe("getLevelBadgeVariant", () => {
	it("returns 'error' for error level", () => {
		expect(getLevelBadgeVariant("error")).toBe("error");
	});

	it("returns 'warning' for warning level", () => {
		expect(getLevelBadgeVariant("warning")).toBe("warning");
	});

	it("returns 'info' for info level", () => {
		expect(getLevelBadgeVariant("info")).toBe("info");
	});

	it("returns 'muted' for debug level (distinct from info)", () => {
		expect(getLevelBadgeVariant("debug")).toBe("muted");
	});

	it("returns 'info' for unknown level", () => {
		expect(getLevelBadgeVariant("unknown")).toBe("info");
		expect(getLevelBadgeVariant("")).toBe("info");
	});
});

describe("getSourceBadgeClasses", () => {
	it("returns purple classes for auth", () => {
		const result = getSourceBadgeClasses("auth");
		expect(result).toContain("bg-purple-900/30");
		expect(result).toContain("text-purple-400");
	});

	it("returns cyan classes for proxy", () => {
		const result = getSourceBadgeClasses("proxy");
		expect(result).toContain("bg-cyan-900/30");
		expect(result).toContain("text-cyan-400");
	});

	it("returns teal classes for resolve", () => {
		const result = getSourceBadgeClasses("resolve");
		expect(result).toContain("bg-teal-900/30");
		expect(result).toContain("text-teal-400");
	});

	it("returns emerald classes for discovery", () => {
		const result = getSourceBadgeClasses("discovery");
		expect(result).toContain("bg-emerald-900/30");
		expect(result).toContain("text-emerald-400");
	});

	it("returns slate classes for failover", () => {
		const result = getSourceBadgeClasses("failover");
		expect(result).toContain("bg-slate-700/50");
		expect(result).toContain("text-slate-300");
	});

	it("returns amber classes for ratelimit", () => {
		const result = getSourceBadgeClasses("ratelimit");
		expect(result).toContain("bg-amber-900/30");
		expect(result).toContain("text-amber-400");
	});

	it("returns pink classes for vkey", () => {
		const result = getSourceBadgeClasses("vkey");
		expect(result).toContain("bg-pink-900/30");
		expect(result).toContain("text-pink-400");
	});

	it("returns pink classes for admin", () => {
		const result = getSourceBadgeClasses("admin");
		expect(result).toContain("bg-pink-900/30");
		expect(result).toContain("text-pink-400");
	});

	it("returns indigo classes for settings", () => {
		const result = getSourceBadgeClasses("settings");
		expect(result).toContain("bg-indigo-900/30");
		expect(result).toContain("text-indigo-400");
	});

	it("returns a bolder indigo for configsync (HA), distinct from settings", () => {
		const result = getSourceBadgeClasses("configsync");
		expect(result).toContain("bg-indigo-500/30");
		expect(result).toContain("text-indigo-200");
		// Must not collide with the muted indigo used for `settings`.
		expect(result).not.toBe(getSourceBadgeClasses("settings"));
	});

	it("returns the same HA classes for fleet as for configsync", () => {
		expect(getSourceBadgeClasses("fleet")).toBe(
			getSourceBadgeClasses("configsync"),
		);
	});

	it("returns violet classes for events", () => {
		const result = getSourceBadgeClasses("events");
		expect(result).toContain("bg-violet-900/30");
		expect(result).toContain("text-violet-400");
	});

	it("returns sky classes for docker", () => {
		const result = getSourceBadgeClasses("docker");
		expect(result).toContain("bg-sky-900/30");
		expect(result).toContain("text-sky-400");
	});

	it("returns lime classes for keycache", () => {
		const result = getSourceBadgeClasses("keycache");
		expect(result).toContain("bg-lime-900/30");
		expect(result).toContain("text-lime-400");
	});

	it("returns lime classes for model", () => {
		const result = getSourceBadgeClasses("model");
		expect(result).toContain("bg-lime-900/30");
		expect(result).toContain("text-lime-400");
	});

	it("returns lime classes for provider", () => {
		const result = getSourceBadgeClasses("provider");
		expect(result).toContain("bg-lime-900/30");
		expect(result).toContain("text-lime-400");
	});

	it("returns lime classes for cache", () => {
		const result = getSourceBadgeClasses("cache");
		expect(result).toContain("bg-lime-900/30");
		expect(result).toContain("text-lime-400");
	});

	it("returns lime classes for db", () => {
		const result = getSourceBadgeClasses("db");
		expect(result).toContain("bg-lime-900/30");
		expect(result).toContain("text-lime-400");
	});

	it("returns fuchsia classes for access", () => {
		const result = getSourceBadgeClasses("access");
		expect(result).toContain("bg-fuchsia-900/30");
		expect(result).toContain("text-fuchsia-400");
	});

	it("returns blue classes for server", () => {
		const result = getSourceBadgeClasses("server");
		expect(result).toContain("bg-blue-900/30");
		expect(result).toContain("text-blue-400");
	});

	it("returns blue classes for startup", () => {
		const result = getSourceBadgeClasses("startup");
		expect(result).toContain("bg-blue-900/30");
		expect(result).toContain("text-blue-400");
	});

	it("returns blue classes for retention", () => {
		const result = getSourceBadgeClasses("retention");
		expect(result).toContain("bg-blue-900/30");
		expect(result).toContain("text-blue-400");
	});

	it("returns orange classes for circuit-breaker", () => {
		const result = getSourceBadgeClasses("circuit-breaker");
		expect(result).toContain("bg-orange-900/30");
		expect(result).toContain("text-orange-400");
	});

	it("returns rose classes for modelsdev", () => {
		const result = getSourceBadgeClasses("modelsdev");
		expect(result).toContain("bg-rose-900/30");
		expect(result).toContain("text-rose-400");
	});

	it("returns gray classes for applogs", () => {
		const result = getSourceBadgeClasses("applogs");
		expect(result).toContain("bg-gray-700/30");
		expect(result).toContain("text-gray-400");
	});

	it("returns default classes for unknown source", () => {
		const result = getSourceBadgeClasses("unknown");
		expect(result).toBe("bg-gray-800/30 text-gray-400");
	});
});

describe("formatTimestamp", () => {
	it("formats valid ISO date string", () => {
		const result = formatTimestamp("2024-01-15T10:30:45Z");
		expect(result).toBe(
			new Date("2024-01-15T10:30:45Z").toLocaleString("en-US", {
				year: "numeric",
				month: "2-digit",
				day: "2-digit",
				hour: "2-digit",
				minute: "2-digit",
				second: "2-digit",
				hour12: false,
			}),
		);
	});

	it("returns original string for invalid date", () => {
		const result = formatTimestamp("not-a-date");
		expect(result).toBe("not-a-date");
	});

	it("returns original string for empty string", () => {
		const result = formatTimestamp("");
		expect(result).toBe("");
	});
});
