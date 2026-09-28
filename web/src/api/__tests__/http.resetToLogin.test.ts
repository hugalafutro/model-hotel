import { afterEach, describe, expect, it, vi } from "vitest";
import { resetToLogin } from "../http";

// The quota payloads mirrored into localStorage carry provider account details
// of the session that ends here; the next login on this browser must not find
// them. Other keys (chat history, preferences) are not the session's.
describe("resetToLogin", () => {
	const original = window.location;
	afterEach(() => {
		Object.defineProperty(window, "location", {
			value: original,
			configurable: true,
		});
		localStorage.clear();
	});

	it("drops the model-hotel: mirror and nothing else", () => {
		const reload = vi.fn();
		Object.defineProperty(window, "location", {
			value: { ...original, reload },
			configurable: true,
		});
		localStorage.setItem("model-hotel:ollama-cloud-account", '{"email":"x"}');
		localStorage.setItem("model-hotel:nanogpt-usage", "{}");
		localStorage.setItem("chatMessages", "[]");

		resetToLogin();

		expect(localStorage.getItem("model-hotel:ollama-cloud-account")).toBeNull();
		expect(localStorage.getItem("model-hotel:nanogpt-usage")).toBeNull();
		expect(localStorage.getItem("chatMessages")).toBe("[]");
		expect(reload).toHaveBeenCalled();
	});
});
