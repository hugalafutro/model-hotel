import { beforeEach, describe, expect, it } from "vitest";
import { consumeOidcError, consumeOidcToken } from "../oidc";

function setHash(h: string) {
	window.location.hash = h;
}

function resetUrl() {
	window.history.replaceState(null, "", "/");
}

describe("consumeOidcToken", () => {
	beforeEach(() => {
		localStorage.clear();
		resetUrl();
	});

	it("returns false when there is no token fragment", () => {
		expect(consumeOidcToken()).toBe(false);
		expect(localStorage.getItem("adminToken")).toBeNull();
	});

	it("stores the decoded token and scrubs the fragment", () => {
		setHash("#oidc_token=sk-abc%20123");
		expect(consumeOidcToken()).toBe(true);
		expect(localStorage.getItem("adminToken")).toBe("sk-abc 123");
		expect(window.location.hash).toBe("");
	});

	it("does not crash on a malformed fragment and scrubs it", () => {
		setHash("#oidc_token=%");
		expect(consumeOidcToken()).toBe(false);
		expect(localStorage.getItem("adminToken")).toBeNull();
		expect(window.location.hash).toBe("");
	});

	it("returns false for an empty token", () => {
		setHash("#oidc_token=");
		expect(consumeOidcToken()).toBe(false);
		expect(localStorage.getItem("adminToken")).toBeNull();
	});
});

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
