import { describe, expect, it } from "vitest";
import { isCancelled } from "../logHelpers";

describe("isCancelled", () => {
	it("returns false for undefined", () => {
		expect(isCancelled()).toBe(false);
	});

	it("returns false for empty string", () => {
		expect(isCancelled("")).toBe(false);
	});

	it("returns false for unrelated error message", () => {
		expect(isCancelled("500 Internal Server Error")).toBe(false);
	});

	it("returns true for message containing cancel", () => {
		expect(isCancelled("context canceled")).toBe(true);
	});

	it("returns true for message containing disconnect", () => {
		expect(isCancelled("client disconnected")).toBe(true);
	});

	it("returns true for upstream request timed out", () => {
		expect(isCancelled("upstream request timed out")).toBe(true);
	});

	it("returns true for param-strip retry timed out", () => {
		expect(isCancelled("param-strip retry timed out")).toBe(true);
	});

	it("is case-insensitive", () => {
		expect(isCancelled("Context CANCELED")).toBe(true);
		expect(isCancelled("DISCONNECTED")).toBe(true);
	});

	it("returns true when keyword is part of longer message", () => {
		expect(isCancelled("the request was cancelled by the user")).toBe(true);
	});
});
