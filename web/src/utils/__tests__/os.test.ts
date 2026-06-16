import { afterEach, describe, expect, it, vi } from "vitest";
import { detectOS } from "../os";

function stubNavigator(props: {
	platform?: string;
	userAgent?: string;
	uaDataPlatform?: string;
}) {
	vi.stubGlobal("navigator", {
		platform: props.platform ?? "",
		userAgent: props.userAgent ?? "",
		...(props.uaDataPlatform !== undefined
			? { userAgentData: { platform: props.uaDataPlatform } }
			: {}),
	});
}

describe("detectOS", () => {
	afterEach(() => {
		vi.unstubAllGlobals();
	});

	it("prefers userAgentData.platform when present", () => {
		stubNavigator({ uaDataPlatform: "macOS", userAgent: "irrelevant" });
		expect(detectOS()).toBe("macos");
	});

	it("detects Windows", () => {
		stubNavigator({
			platform: "Win32",
			userAgent: "Mozilla/5.0 (Windows NT 10.0)",
		});
		expect(detectOS()).toBe("windows");
	});

	it("detects macOS from the user agent", () => {
		stubNavigator({
			platform: "MacIntel",
			userAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)",
		});
		expect(detectOS()).toBe("macos");
	});

	it("treats iOS as macOS for icon purposes", () => {
		stubNavigator({
			platform: "iPhone",
			userAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)",
		});
		expect(detectOS()).toBe("macos");
	});

	it("detects Linux (and Android/ChromeOS as Linux)", () => {
		stubNavigator({ platform: "Linux x86_64", userAgent: "X11; Linux" });
		expect(detectOS()).toBe("linux");
		stubNavigator({ platform: "", userAgent: "Linux; Android 14" });
		expect(detectOS()).toBe("linux");
	});

	it("falls back to unknown for unrecognized platforms", () => {
		stubNavigator({ platform: "BeOS", userAgent: "Haiku" });
		expect(detectOS()).toBe("unknown");
	});
});
