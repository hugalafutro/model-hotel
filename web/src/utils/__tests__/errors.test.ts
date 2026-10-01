import { describe, expect, it } from "vitest";
import { asError, errorMessage, errorStatus, isAbortError } from "../errors";

describe("errorMessage", () => {
	it("reads an Error's message", () => {
		expect(errorMessage(new Error("boom"))).toBe("boom");
	});

	it("reads a duck-typed message off a plain object", () => {
		// A rejection whose Error class came from another bundle fails
		// `instanceof Error`; the login screen still has to see the body it
		// carries, e.g. the totp_required marker.
		expect(errorMessage({ status: 401, message: "totp_required" })).toBe(
			"totp_required",
		);
	});

	it("stringifies anything else", () => {
		expect(errorMessage("plain")).toBe("plain");
		expect(errorMessage(404)).toBe("404");
		expect(errorMessage({ message: 42 }, "fallback")).toBe("[object Object]");
	});

	it("falls back for an empty or absent message", () => {
		expect(errorMessage(new Error(""), "fallback")).toBe("fallback");
		expect(errorMessage(undefined, "fallback")).toBe("fallback");
		expect(errorMessage(null)).toBe("");
	});
});

describe("errorStatus", () => {
	it("reads a numeric status", () => {
		expect(errorStatus({ status: 409 })).toBe(409);
	});

	it("is undefined without one", () => {
		expect(errorStatus({ status: "409" })).toBeUndefined();
		expect(errorStatus(new Error("no status"))).toBeUndefined();
		expect(errorStatus(null)).toBeUndefined();
		expect(errorStatus("string")).toBeUndefined();
	});
});

describe("asError", () => {
	it("passes an Error through unchanged", () => {
		const err = new Error("boom");
		expect(asError(err)).toBe(err);
	});

	it("wraps anything else", () => {
		expect(asError("boom").message).toBe("boom");
	});

	it("spells a nullish rejection out instead of leaving it blank", () => {
		expect(asError(undefined).message).toBe("undefined");
		expect(asError(null).message).toBe("null");
	});
});

describe("isAbortError", () => {
	it("matches an abort by name, whatever its realm or type", () => {
		expect(isAbortError(new DOMException("stop", "AbortError"))).toBe(true);
		expect(isAbortError({ name: "AbortError" })).toBe(true);
	});

	it("rejects anything else", () => {
		expect(isAbortError(new Error("boom"))).toBe(false);
		expect(isAbortError(null)).toBe(false);
		expect(isAbortError("AbortError")).toBe(false);
	});
});
