import { describe, expect, it, vi } from "vitest";
import { readSSEStream } from "../sse";

describe("readSSEStream", () => {
	const createMockReader = (chunks: Uint8Array[]) => {
		let index = 0;
		return {
			read: async () => {
				if (index < chunks.length) {
					return { done: false, value: chunks[index++] };
				}
				return { done: true, value: undefined };
			},
			cancel: vi.fn(),
		} as unknown as ReadableStreamDefaultReader<Uint8Array>;
	};

	const encoder = new TextEncoder();

	it("parses single line data events", async () => {
		const chunks = [encoder.encode('data: {"content":"hello"}\n\n')];
		const reader = createMockReader(chunks);
		const receivedChunks: unknown[] = [];

		const result = await readSSEStream({
			reader,
			onChunk: (parsed) => receivedChunks.push(parsed),
		});

		expect(receivedChunks).toEqual([{ content: "hello" }]);
		expect(result.sawDone).toBe(false);
		expect(result.aborted).toBe(false);
	});

	it("parses multi-line data fields", async () => {
		const chunks = [
			encoder.encode('data: {"content":"line1"}\n'),
			encoder.encode('data: {"content":"line2"}\n\n'),
		];
		const reader = createMockReader(chunks);
		const receivedChunks: unknown[] = [];

		const result = await readSSEStream({
			reader,
			onChunk: (parsed) => receivedChunks.push(parsed),
		});

		expect(receivedChunks).toEqual([
			{ content: "line1" },
			{ content: "line2" },
		]);
		expect(result.sawDone).toBe(false);
	});

	it("handles [DONE] sentinel", async () => {
		const chunks = [
			encoder.encode('data: {"content":"hello"}\n\n'),
			encoder.encode("data: [DONE]\n\n"),
		];
		const reader = createMockReader(chunks);
		const receivedChunks: unknown[] = [];

		const result = await readSSEStream({
			reader,
			onChunk: (parsed) => receivedChunks.push(parsed),
		});

		expect(receivedChunks).toEqual([{ content: "hello" }]);
		expect(result.sawDone).toBe(true);
		expect(result.aborted).toBe(false);
	});

	it("strips BOM characters from first line", async () => {
		const bom = "\uFEFF";
		const chunks = [encoder.encode(`${bom}data: {"content":"hello"}\n\n`)];
		const reader = createMockReader(chunks);
		const receivedChunks: unknown[] = [];

		const result = await readSSEStream({
			reader,
			onChunk: (parsed) => receivedChunks.push(parsed),
		});

		expect(receivedChunks).toEqual([{ content: "hello" }]);
		expect(result.sawDone).toBe(false);
	});

	it("calls onError on reading errors", async () => {
		const mockReader = {
			read: async () => {
				throw new Error("Stream error");
			},
			cancel: vi.fn(),
		} as unknown as ReadableStreamDefaultReader<Uint8Array>;

		const receivedChunks: unknown[] = [];

		await expect(
			readSSEStream({
				reader: mockReader,
				onChunk: (parsed) => receivedChunks.push(parsed),
			}),
		).rejects.toThrow("Stream error");
	});

	it("respects abort signal", async () => {
		const abortController = new AbortController();
		const chunks = [encoder.encode('data: {"content":"hello"}\n\n')];
		const reader = createMockReader(chunks);
		const receivedChunks: unknown[] = [];

		// Abort before reading completes
		abortController.abort();

		const result = await readSSEStream({
			reader,
			signal: abortController.signal,
			onChunk: (parsed) => receivedChunks.push(parsed),
		});

		expect(result.aborted).toBe(true);
	});

	it("handles data: without space (LM Studio format)", async () => {
		const chunks = [encoder.encode('data:{"content":"hello"}\n\n')];
		const reader = createMockReader(chunks);
		const receivedChunks: unknown[] = [];

		const result = await readSSEStream({
			reader,
			onChunk: (parsed) => receivedChunks.push(parsed),
		});

		expect(receivedChunks).toEqual([{ content: "hello" }]);
		expect(result.sawDone).toBe(false);
	});

	it("handles custom doneSentinel", async () => {
		const chunks = [
			encoder.encode('data: {"content":"hello"}\n\n'),
			encoder.encode("data: CUSTOM_DONE\n\n"),
		];
		const reader = createMockReader(chunks);
		const receivedChunks: unknown[] = [];

		const result = await readSSEStream({
			reader,
			onChunk: (parsed) => receivedChunks.push(parsed),
			doneSentinel: "CUSTOM_DONE",
		});

		expect(receivedChunks).toEqual([{ content: "hello" }]);
		expect(result.sawDone).toBe(true);
	});

	it("skips doneSentinel check when set to null", async () => {
		const chunks = [
			encoder.encode('data: {"content":"hello"}\n\n'),
			encoder.encode("data: [DONE]\n\n"),
		];
		const reader = createMockReader(chunks);
		const receivedChunks: unknown[] = [];

		const result = await readSSEStream({
			reader,
			onChunk: (parsed) => receivedChunks.push(parsed),
			doneSentinel: null,
		});

		// [DONE] should be parsed as JSON (and fail silently)
		expect(receivedChunks).toEqual([{ content: "hello" }]);
		expect(result.sawDone).toBe(false);
	});

	it("handles leading whitespace and carriage returns", async () => {
		const chunks = [encoder.encode('\r\n  data: {"content":"hello"}\n\n')];
		const reader = createMockReader(chunks);
		const receivedChunks: unknown[] = [];

		const result = await readSSEStream({
			reader,
			onChunk: (parsed) => receivedChunks.push(parsed),
		});

		expect(receivedChunks).toEqual([{ content: "hello" }]);
		expect(result.sawDone).toBe(false);
	});

	it("ignores malformed JSON silently", async () => {
		const chunks = [
			encoder.encode('data: {"invalid json}\n\n'),
			encoder.encode('data: {"content":"valid"}\n\n'),
		];
		const reader = createMockReader(chunks);
		const receivedChunks: unknown[] = [];

		const result = await readSSEStream({
			reader,
			onChunk: (parsed) => receivedChunks.push(parsed),
		});

		// Only valid JSON should be received
		expect(receivedChunks).toEqual([{ content: "valid" }]);
		expect(result.sawDone).toBe(false);
	});

	it("handles empty data lines", async () => {
		const chunks = [
			encoder.encode('data: {"content":"hello"}\n\n'),
			encoder.encode("data:\n\n"),
			encoder.encode('data: {"content":"world"}\n\n'),
		];
		const reader = createMockReader(chunks);
		const receivedChunks: unknown[] = [];

		const result = await readSSEStream({
			reader,
			onChunk: (parsed) => receivedChunks.push(parsed),
		});

		expect(receivedChunks).toEqual([
			{ content: "hello" },
			{ content: "world" },
		]);
		expect(result.sawDone).toBe(false);
	});

	it("handles chunks split across multiple reads", async () => {
		const fullData = 'data: {"content":"hello world"}\n\n';
		const midPoint = Math.floor(fullData.length / 2);
		const chunks = [
			encoder.encode(fullData.slice(0, midPoint)),
			encoder.encode(fullData.slice(midPoint)),
		];
		const reader = createMockReader(chunks);
		const receivedChunks: unknown[] = [];

		const result = await readSSEStream({
			reader,
			onChunk: (parsed) => receivedChunks.push(parsed),
		});

		expect(receivedChunks).toEqual([{ content: "hello world" }]);
		expect(result.sawDone).toBe(false);
	});

	it("handles reasoning_content field", async () => {
		const chunks = [
			encoder.encode(
				'data: {"choices":[{"delta":{"reasoning_content":"thinking"}}]}\n\n',
			),
		];
		const reader = createMockReader(chunks);
		const receivedChunks: unknown[] = [];

		const result = await readSSEStream({
			reader,
			onChunk: (parsed) => receivedChunks.push(parsed),
		});

		expect(receivedChunks).toEqual([
			{ choices: [{ delta: { reasoning_content: "thinking" } }] },
		]);
		expect(result.sawDone).toBe(false);
	});

	it("handles usage field", async () => {
		const chunks = [
			encoder.encode(
				'data: {"usage":{"prompt_tokens":10,"completion_tokens":20}}\n\n',
			),
		];
		const reader = createMockReader(chunks);
		const receivedChunks: unknown[] = [];

		const result = await readSSEStream({
			reader,
			onChunk: (parsed) => receivedChunks.push(parsed),
		});

		expect(receivedChunks).toEqual([
			{ usage: { prompt_tokens: 10, completion_tokens: 20 } },
		]);
		expect(result.sawDone).toBe(false);
	});
});
