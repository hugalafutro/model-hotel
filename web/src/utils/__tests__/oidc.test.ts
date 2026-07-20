import { beforeEach, describe, expect, it } from "vitest";
import { consumeOidcError } from "../oidc";

function setHash(h: string) {
	window.location.hash = h;
}

function resetUrl() {
	window.history.replaceState(null, "", "/");
}

describe("consumeOidcError", () => {
	beforeEach(() => {
		resetUrl();
	});

	it("returns null when there is no error fragment", () => {
		expect(consumeOidcError()).toBeNull();
	});

	it("returns the code and scrubs the fragment", () => {
		setHash("#oidc_error=throttled");
		expect(consumeOidcError()).toBe("throttled");
		expect(window.location.hash).toBe("");
	});

	it("falls back to 'unknown' on a malformed code without crashing", () => {
		setHash("#oidc_error=%");
		expect(consumeOidcError()).toBe("unknown");
		expect(window.location.hash).toBe("");
	});
});
