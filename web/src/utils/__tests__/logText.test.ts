import { describe, expect, it } from "vitest";
import { decodeLogEscapes } from "../logText";

describe("decodeLogEscapes", () => {
	it("turns the \\x20 space escaping back into spaces for display", () => {
		expect(
			decodeLogEscapes('account fetched provider="Ollama\\x20Cloud" plan=pro'),
		).toBe('account fetched provider="Ollama Cloud" plan=pro');
	});

	it("decodes multiple escaped spaces in one value", () => {
		expect(decodeLogEscapes('model="My\\x20Great\\x20Model"')).toBe(
			'model="My Great Model"',
		);
	});

	it("preserves a literal \\x20 whose backslash is itself escaped", () => {
		// The original value contained a literal `\x20`; quoteLogValue escaped
		// its backslash to `\\`, so the line shows `\\x20` and must stay put.
		expect(decodeLogEscapes('path="C:\\\\x20dir"')).toBe('path="C:\\\\x20dir"');
	});

	it("decodes an escaped space after a literal backslash", () => {
		// `\\` (a literal backslash) followed by our `\x20` space escape.
		expect(decodeLogEscapes('v="a\\\\\\x20b"')).toBe('v="a\\\\ b"');
	});

	it("leaves messages without escapes untouched", () => {
		expect(decodeLogEscapes("request start client_ip=10.0.0.1")).toBe(
			"request start client_ip=10.0.0.1",
		);
	});
});
