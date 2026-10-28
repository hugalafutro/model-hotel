import { describe, expect, it } from "vitest";
import {
	decodeLogEscapes,
	displayLogMessage,
	parseLogAttrs,
	splitLogMessage,
} from "../logText";

describe("parseLogAttrs", () => {
	it("reads an access line into ordered pairs", () => {
		expect(
			parseLogAttrs(
				"method=GET host=mh.example remote=192.168.1.141 status=200 bytes=1704 duration=331.127816ms path=/api/logs/app",
			),
		).toEqual([
			{ key: "method", value: "GET" },
			{ key: "host", value: "mh.example" },
			{ key: "remote", value: "192.168.1.141" },
			{ key: "status", value: "200" },
			{ key: "bytes", value: "1704" },
			{ key: "duration", value: "331.127816ms" },
			{ key: "path", value: "/api/logs/app" },
		]);
	});

	it("unquotes a value with spaces and escaped quotes", () => {
		expect(
			parseLogAttrs('provider="Open Router" error="said \\"no\\" twice" n=2'),
		).toEqual([
			{ key: "provider", value: "Open Router" },
			{ key: "error", value: 'said "no" twice' },
			{ key: "n", value: "2" },
		]);
	});

	it("decodes Go quoted-string escapes, not just the next character", () => {
		expect(
			parseLogAttrs(
				'error="line one\\nline two\\ttab" path="\\x00nul \\u00e9 \\U0001F600" odd="\\q"',
			),
		).toEqual([
			{ key: "error", value: "line one\nline two\ttab" },
			{ key: "path", value: "\x00nul é 😀" },
			{ key: "odd", value: "q" },
		]);
	});

	it("keeps an out-of-range code point escape as text instead of throwing", () => {
		expect(parseLogAttrs('v="\\U00110000 ok"')).toEqual([
			{ key: "v", value: "U00110000 ok" },
		]);
	});

	it("skips tokens that are not key=value and keeps the rest", () => {
		expect(parseLogAttrs("loose words count=3")).toEqual([
			{ key: "count", value: "3" },
		]);
		expect(parseLogAttrs("nothing here")).toEqual([]);
	});

	it("keeps an empty value", () => {
		expect(parseLogAttrs('a= b=""')).toEqual([
			{ key: "a", value: "" },
			{ key: "b", value: "" },
		]);
	});
});

describe("splitLogMessage", () => {
	it("splits at the recorded boundary", () => {
		const msg = "request served method=GET status=200";
		expect(splitLogMessage(msg, "request served".length)).toEqual({
			head: "request served",
			attrs: [
				{ key: "method", value: "GET" },
				{ key: "status", value: "200" },
			],
		});
	});

	it("keeps the whole message without a boundary or without attributes", () => {
		expect(splitLogMessage("plain line", 0)).toEqual({
			head: "plain line",
			attrs: [],
		});
		expect(splitLogMessage("head and tail", 4)).toEqual({
			head: "head and tail",
			attrs: [],
		});
	});
});

describe("decodeLogEscapes", () => {
	it("turns an escaped space inside a quoted value back into a space", () => {
		expect(decodeLogEscapes('a="x\\x20y" b=1')).toBe('a="x y" b=1');
	});

	it("leaves an escaped space outside quotes alone", () => {
		expect(decodeLogEscapes("a=x\\x20y")).toBe("a=x\\x20y");
	});

	it("copies an escaped quote through without closing the value", () => {
		// The \" does not toggle the quote state, so the \x20 after it is
		// still inside the value and decodes.
		expect(decodeLogEscapes('e="said \\"no\\"\\x20twice"')).toBe(
			'e="said \\"no\\" twice"',
		);
	});

	it("reads a backslash run by parity: only an odd run escapes what follows", () => {
		// Three backslashes: one pair stays, the third escapes the space.
		expect(decodeLogEscapes('p="C:\\\\\\x20dir"')).toBe('p="C:\\\\ dir"');
		// Two backslashes: a literal pair, and x20 is plain text after it.
		expect(decodeLogEscapes('p="C:\\\\x20dir"')).toBe('p="C:\\\\x20dir"');
	});
});

describe("displayLogMessage", () => {
	it("decodes only from the attribute boundary and only when flagged", () => {
		const msg = 'head\\x20kept a="x\\x20y"';
		expect(displayLogMessage(msg, true, "head\\x20kept ".length)).toBe(
			'head\\x20kept a="x y"',
		);
		expect(displayLogMessage(msg, false, 0)).toBe(msg);
		expect(displayLogMessage(msg, true, 999)).toBe(msg);
	});
});
