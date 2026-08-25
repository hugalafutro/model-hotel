import { readCookie } from "@web-shared/cookies";
import { afterEach, describe, expect, it } from "vitest";

// readCookie backs both frontends' getCsrfToken. jsdom keeps a real
// document.cookie, so each case writes one and expires it again afterwards.
const written: string[] = [];

function setCookie(name: string, value: string) {
	document.cookie = `${name}=${value}; path=/`;
	written.push(name);
}

afterEach(() => {
	for (const name of written.splice(0)) {
		document.cookie = `${name}=; path=/; max-age=0`;
	}
});

describe("readCookie", () => {
	it("returns null when the cookie is absent", () => {
		expect(readCookie("no_such_cookie")).toBeNull();
	});

	it("returns the value of the named cookie", () => {
		setCookie("read_plain", "value");
		expect(readCookie("read_plain")).toBe("value");
	});

	it("percent-decodes the value", () => {
		setCookie("read_encoded", "a%20b%2Bc");
		expect(readCookie("read_encoded")).toBe("a b+c");
	});

	it("picks the named cookie out of several", () => {
		setCookie("read_first", "one");
		setCookie("read_second", "two");
		expect(readCookie("read_second")).toBe("two");
		expect(readCookie("read_first")).toBe("one");
	});

	it("matches the whole name, not a suffix of a longer one", () => {
		setCookie("prefix_read_suffix", "wrong");
		expect(readCookie("read_suffix")).toBeNull();
	});

	it("treats regex metacharacters in the name as literal text", () => {
		// "." and "*" are legal cookie-name characters and regex metacharacters
		// both; unescaped, "a.c" would match a stored "abc".
		setCookie("abc", "wrong");
		expect(readCookie("a.c")).toBeNull();
	});
});
