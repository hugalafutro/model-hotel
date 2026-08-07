import { afterEach, describe, expect, it } from "vitest";
import { consumeOidcError } from "../oidc";

// consumeOidcError reads a failed SSO callback's code from the URL fragment and
// scrubs it. Drive it by setting window.location.hash directly (jsdom keeps a
// live location) and assert on the return value + the scrubbed fragment. A
// SUCCESSFUL callback carries no fragment at all: the session is already in the
// HttpOnly cookie and the redirect URL is clean.

afterEach(() => {
	window.location.hash = "";
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

	it("returns null and leaves an unrelated fragment alone", () => {
		window.location.hash = "#something-else";

		expect(consumeOidcError()).toBeNull();
		expect(window.location.hash).toBe("#something-else");
	});

	it("returns 'unknown' without throwing on a malformed percent-encoded code", () => {
		window.location.hash = "#oidc_error=%E0%A4%A";

		expect(consumeOidcError()).toBe("unknown");
		expect(window.location.hash).toBe("");
	});
});
