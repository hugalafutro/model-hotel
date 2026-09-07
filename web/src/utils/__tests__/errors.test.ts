import { describe, expect, it } from "vitest";
import { asError, errorMessage, errorStatus } from "../errors";

describe("errorMessage", () => {
	it("reads an Error's message", () => {
		expect(errorMessage(new Error("boom"))).toBe("boom");
	});

	it("stringifies anything else", () => {
		expect(errorMessage("plain")).toBe("plain");
		expect(errorMessage(404)).toBe("404");
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
		expect(asError(undefined).message).toBe("");
	});
});
