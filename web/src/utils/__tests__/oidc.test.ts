import { consumeOidcError } from "@web-shared/oidc";
import { beforeEach, describe, expect, it } from "vitest";

// consumeOidcError reads a failed SSO callback's code from the URL fragment and
// scrubs it. Drive it by setting window.location.hash directly (jsdom keeps a
// live location) and assert on the return value + the scrubbed fragment. A
// SUCCESSFUL callback carries no fragment at all: the session is already in the
// HttpOnly cookie and the redirect URL is clean.

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

	it("returns the decoded error code", () => {
		setHash("#oidc_error=access_denied");
		expect(consumeOidcError()).toBe("access_denied");
		expect(window.location.hash).toBe("");
	});

	it("falls back to 'unknown' for an empty error code", () => {
		setHash("#oidc_error=");
		expect(consumeOidcError()).toBe("unknown");
	});

	it("returns null and leaves an unrelated fragment alone", () => {
		setHash("#something-else");
		expect(consumeOidcError()).toBeNull();
		expect(window.location.hash).toBe("#something-else");
	});

	it("falls back to 'unknown' on a malformed code without crashing", () => {
		setHash("#oidc_error=%");
		expect(consumeOidcError()).toBe("unknown");
		expect(window.location.hash).toBe("");
	});

	it("returns 'unknown' without throwing on a truncated percent-encoded code", () => {
		setHash("#oidc_error=%E0%A4%A");
		expect(consumeOidcError()).toBe("unknown");
		expect(window.location.hash).toBe("");
	});
});
