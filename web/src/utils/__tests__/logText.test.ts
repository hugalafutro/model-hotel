import { describe, expect, it } from "vitest";
import { parseLogAttrs, splitLogMessage } from "../logText";

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
