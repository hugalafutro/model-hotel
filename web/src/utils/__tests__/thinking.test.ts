import { describe, expect, it } from "vitest";
import { extractThinking, sanitizeDelta } from "../thinking";

describe("extractThinking", () => {
	it("returns empty thinking and original content when no tags present", () => {
		const result = extractThinking("Hello world");
		expect(result.thinking).toBe("");
		expect(result.content).toBe("Hello world");
	});

	it("extracts single thinking block with remainder", () => {
		const result = extractThinking("<thinking>inner thoughts</thinking>rest");
		expect(result.thinking).toBe("inner thoughts");
		expect(result.content).toBe("rest");
	});

	it("extracts only the first thinking block and removes all tags from content", () => {
		const result = extractThinking(
			"<thinking>a</thinking>middle<thinking>b</thinking>",
		);
		// Function extracts first thinking block, then strips all thinking tags from content
		expect(result.thinking).toBe("a");
		expect(result.content).toBe("middleb");
	});

	it("handles empty thinking tags", () => {
		const result = extractThinking("<thinking></thinking>");
		expect(result.thinking).toBe("");
		expect(result.content).toBe("");
	});

	it("handles whitespace inside tags", () => {
		const result = extractThinking(
			"<thinking>\n  some thoughts\n</thinking>content",
		);
		expect(result.thinking).toBe("some thoughts");
		expect(result.content).toBe("content");
	});

	it("is case-insensitive for closing tags", () => {
		const result = extractThinking("<thought>content</thOUGHT>");
		expect(result.thinking).toBe("content");
		expect(result.content).toBe("");
	});

	it("handles start_thought variant (with matching close tag)", () => {
		// Note: closing tag uses same name as opening (start_thought), not end_thought
		const result = extractThinking(
			"<start_thought>content</start_thought>rest",
		);
		expect(result.thinking).toBe("content");
		expect(result.content).toBe("rest");
	});

	it("handles think variant", () => {
		const result = extractThinking("<think>content</think>rest");
		expect(result.thinking).toBe("content");
		expect(result.content).toBe("rest");
	});

	it("handles fence syntax << >>", () => {
		const result = extractThinking("<<\ninner thoughts\n>>\ncontent");
		expect(result.thinking).toBe("inner thoughts");
		expect(result.content).toBe("content");
	});

	it("handles mixed content before and after tags", () => {
		const result = extractThinking("prefix<thinking>thought</thinking>suffix");
		expect(result.thinking).toBe("thought");
		expect(result.content).toBe("prefixsuffix");
	});

	it("handles partial thinking tag at end of content", () => {
		const result = extractThinking("content<thin");
		expect(result.thinking).toBe("");
		expect(result.content).toBe("content");
	});

	it("handles thought tag variant", () => {
		const result = extractThinking("<thought>deep thought</thought>answer");
		expect(result.thinking).toBe("deep thought");
		expect(result.content).toBe("answer");
	});
});

describe("sanitizeDelta", () => {
	it("returns original string when no special tokens present", () => {
		const result = sanitizeDelta("Hello world");
		expect(result).toBe("Hello world");
	});

	it("removes tool calls begin token", () => {
		const result = sanitizeDelta("text<｜tool▁calls▁begin｜>more");
		expect(result).toBe("textmore");
	});

	it("removes tool calls end token", () => {
		const result = sanitizeDelta("text<｜tool▁calls▁end｜>more");
		expect(result).toBe("textmore");
	});

	it("removes tool call begin token", () => {
		const result = sanitizeDelta("text<｜tool▁call▁begin｜>more");
		expect(result).toBe("textmore");
	});

	it("removes multiple special tokens", () => {
		const result = sanitizeDelta(
			"<｜tool▁calls▁begin｜><｜tool▁call▁begin｜>content<｜tool▁calls▁end｜>",
		);
		expect(result).toBe("content");
	});

	it("removes begin of sentence token", () => {
		const result = sanitizeDelta("<｜begin▁of▁sentence｜>content");
		expect(result).toBe("content");
	});

	it("removes end of sentence token", () => {
		const result = sanitizeDelta("content<｜end▁of▁sentence｜>");
		expect(result).toBe("content");
	});

	it("removes Assistant token", () => {
		const result = sanitizeDelta("<｜Assistant｜>response");
		expect(result).toBe("response");
	});
});
