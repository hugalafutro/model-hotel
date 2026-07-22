import { afterEach, describe, expect, it } from "vitest";
import { consumeOidcError, consumeOidcToken } from "../oidc";

// consumeOidcToken / consumeOidcError read the SSO result from the URL fragment
// and scrub it. Drive them by setting window.location.hash directly (jsdom keeps
// a live location) and assert on the stored token slot + the scrubbed fragment.

afterEach(() => {
	window.location.hash = "";
	localStorage.clear();
});

describe("consumeOidcToken", () => {
	it("stores a token from the fragment, scrubs it, and returns true", () => {
		window.location.hash = "#oidc_token=session-abc";

		expect(consumeOidcToken()).toBe(true);
		expect(localStorage.getItem("fdAuthToken")).toBe("session-abc");
		expect(window.location.hash).toBe("");
	});

	it("returns false and stores nothing when the fragment has no token prefix", () => {
		window.location.hash = "#something-else";

		expect(consumeOidcToken()).toBe(false);
		expect(localStorage.getItem("fdAuthToken")).toBeNull();
	});

	it("returns false when the token is empty after decoding", () => {
		window.location.hash = "#oidc_token=";

		expect(consumeOidcToken()).toBe(false);
		expect(localStorage.getItem("fdAuthToken")).toBeNull();
		expect(window.location.hash).toBe("");
	});

	it("returns false without throwing on a malformed percent-encoded token", () => {
		window.location.hash = "#oidc_token=%E0%A4%A";

		expect(consumeOidcToken()).toBe(false);
		expect(localStorage.getItem("fdAuthToken")).toBeNull();
	});
});

describe("consumeOidcError", () => {
	it("returns the decoded error code and scrubs the fragment", () => {
		window.location.hash = "#oidc_error=access_denied";

		expect(consumeOidcError()).toBe("access_denied");
		expect(window.location.hash).toBe("");
	});

	it("falls back to 'unknown' for an empty error code", () => {
		window.location.hash = "#oidc_error=";

		expect(consumeOidcError()).toBe("unknown");
	});

	it("returns null when the fragment carries no error prefix", () => {
		window.location.hash = "#oidc_token=abc";

		expect(consumeOidcError()).toBeNull();
	});
});
