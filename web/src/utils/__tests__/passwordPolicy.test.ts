import { describe, expect, it } from "vitest";
import { isBreachedPasswordError } from "../passwordPolicy";

describe("isBreachedPasswordError", () => {
	it("matches the backend breach rejection embedded in an ApiError message", () => {
		// Shape produced by fetchOK: "<prefix>: <status> <body>".
		const err = {
			message:
				"Request failed: 400 this password has appeared in a known data breach; choose a different one",
			status: 400,
		};
		expect(isBreachedPasswordError(err)).toBe(true);
	});

	it("does not match other password-policy rejections", () => {
		expect(
			isBreachedPasswordError({
				message: "Request failed: 400 password must be at least 8 characters",
			}),
		).toBe(false);
	});

	it("does not match unrelated errors", () => {
		expect(
			isBreachedPasswordError({
				message: "Request failed: 409 username taken",
			}),
		).toBe(false);
	});

	it("is robust against non-error inputs", () => {
		expect(isBreachedPasswordError(null)).toBe(false);
		expect(isBreachedPasswordError(undefined)).toBe(false);
		expect(isBreachedPasswordError("known data breach")).toBe(false);
		expect(isBreachedPasswordError({})).toBe(false);
		expect(isBreachedPasswordError({ message: 42 })).toBe(false);
	});
});
