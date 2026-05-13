import { describe, expect, it, vi } from "vitest";
import type { ChatMessage } from "../../../api/types";
import { mockChatStream } from "../../../test/helpers";
import { server } from "../../../test/mocks/server";
import {
	buildMessageContent,
	formatTime,
	getApiMessagesForModel,
	streamModelResponse,
} from "../chatStreaming";

describe("formatTime", () => {
	it("formats timestamp as HH:MM", () => {
		const ts = new Date("2024-01-15T14:30:00Z").getTime();
		const result = formatTime(ts);

		expect(result).toMatch(/^\d{1,2}:\d{2}$/);
		expect(result).toContain(":");
	});

	it("formats midnight correctly", () => {
		const ts = new Date("2024-01-15T00:00:00Z").getTime();
		const result = formatTime(ts);

		expect(result).toMatch(/00:00/);
	});

	it("formats noon correctly", () => {
		const ts = new Date("2024-01-15T12:00:00Z").getTime();
		const result = formatTime(ts);

		expect(result).toMatch(/12:00/);
	});

	it("handles different timezones based on locale", () => {
		const ts = Date.now();
		const result = formatTime(ts);

		expect(result).toBeDefined();
		expect(typeof result).toBe("string");
		expect(result.split(":")).toHaveLength(2);
	});
});

describe("buildMessageContent", () => {
	it("returns plain string for message without attachments", () => {
		const msg: ChatMessage = {
			role: "user",
			content: "Hello, world!",
			model: "model-1",
			timestamp: Date.now(),
		};

		const result = buildMessageContent(msg);

		expect(result).toBe("Hello, world!");
	});

	it("returns content array with image_url part for message with imageUrl", () => {
		const msg: ChatMessage = {
			role: "user",
			content: "What is this image?",
			model: "model-1",
			timestamp: Date.now(),
			imageUrl: "data:image/png;base64,abc123",
		};

		const result = buildMessageContent(msg);

		expect(Array.isArray(result)).toBe(true);
		expect(result).toHaveLength(2);
		expect(result[0]).toEqual({
			type: "image_url",
			image_url: { url: "data:image/png;base64,abc123" },
		});
		expect(result[1]).toEqual({
			type: "text",
			text: "What is this image?",
		});
	});

	it("returns content array with input_audio part for message with audioAttachment", () => {
		const msg: ChatMessage = {
			role: "user",
			content: "Listen to this",
			model: "model-1",
			timestamp: Date.now(),
			audioAttachment: {
				data: "base64audio",
				format: "wav",
			},
		};

		const result = buildMessageContent(msg);

		expect(Array.isArray(result)).toBe(true);
		expect(result).toHaveLength(2);
		expect(result[0]).toEqual({
			type: "input_audio",
			input_audio: {
				data: "base64audio",
				format: "wav",
			},
		});
		expect(result[1]).toEqual({
			type: "text",
			text: "Listen to this",
		});
	});

	it("returns content array with both image and audio parts", () => {
		const msg: ChatMessage = {
			role: "user",
			content: "Describe this image and audio",
			model: "model-1",
			timestamp: Date.now(),
			imageUrl: "data:image/png;base64,img",
			audioAttachment: {
				data: "base64audio",
				format: "mp3",
			},
		};

		const result = buildMessageContent(msg);

		expect(Array.isArray(result)).toBe(true);
		expect(result).toHaveLength(3);
		expect(result[0]).toEqual({
			type: "image_url",
			image_url: { url: "data:image/png;base64,img" },
		});
		expect(result[1]).toEqual({
			type: "input_audio",
			input_audio: {
				data: "base64audio",
				format: "mp3",
			},
		});
		expect(result[2]).toEqual({
			type: "text",
			text: "Describe this image and audio",
		});
	});

	it("returns only image part when content is empty", () => {
		const msg: ChatMessage = {
			role: "user",
			content: "",
			model: "model-1",
			timestamp: Date.now(),
			imageUrl: "data:image/png;base64,img",
		};

		const result = buildMessageContent(msg);

		expect(Array.isArray(result)).toBe(true);
		expect(result).toHaveLength(1);
		expect(result[0]).toEqual({
			type: "image_url",
			image_url: { url: "data:image/png;base64,img" },
		});
	});

	it("returns only audio part when content is empty", () => {
		const msg: ChatMessage = {
			role: "user",
			content: "",
			model: "model-1",
			timestamp: Date.now(),
			audioAttachment: {
				data: "base64audio",
				format: "wav",
			},
		};

		const result = buildMessageContent(msg);

		expect(Array.isArray(result)).toBe(true);
		expect(result).toHaveLength(1);
		expect(result[0]).toEqual({
			type: "input_audio",
			input_audio: {
				data: "base64audio",
				format: "wav",
			},
		});
	});

	it("handles message with only imageUrl and no text", () => {
		const msg: ChatMessage = {
			role: "user",
			content: "",
			model: "model-1",
			timestamp: Date.now(),
			imageUrl: "https://example.com/image.png",
		};

		const result = buildMessageContent(msg);

		expect(Array.isArray(result)).toBe(true);
		expect(result).toHaveLength(1);
		expect((result[0] as { type: string }).type).toBe("image_url");
	});
});

describe("getApiMessagesForModel", () => {
	it("returns empty array for empty messages", () => {
		const result = getApiMessagesForModel([], "model-1", "");
		expect(result).toEqual([]);
	});

	it("includes system message when persona is provided", () => {
		const messages: ChatMessage[] = [];
		const persona = "You are a helpful assistant.";

		const result = getApiMessagesForModel(messages, "model-1", persona);

		expect(result).toHaveLength(1);
		expect(result[0]).toEqual({
			role: "system",
			content: "You are a helpful assistant.",
		});
	});

	it("does not include system message when persona is empty", () => {
		const messages: ChatMessage[] = [];
		const result = getApiMessagesForModel(messages, "model-1", "");

		expect(result).toEqual([]);
	});

	it("does not include system message when persona is whitespace only", () => {
		const messages: ChatMessage[] = [];
		const result = getApiMessagesForModel(messages, "model-1", "   ");

		expect(result).toEqual([]);
	});

	it("includes user messages with plain content", () => {
		const messages: ChatMessage[] = [
			{
				role: "user",
				content: "Hello",
				model: "model-1",
				timestamp: Date.now(),
			},
		];

		const result = getApiMessagesForModel(messages, "model-1", "");

		expect(result).toHaveLength(1);
		expect(result[0]).toEqual({
			role: "user",
			content: "Hello",
		});
	});

	it("includes user messages with attachments", () => {
		const messages: ChatMessage[] = [
			{
				role: "user",
				content: "What is this?",
				model: "model-1",
				timestamp: Date.now(),
				imageUrl: "data:image/png;base64,img",
			},
		];

		const result = getApiMessagesForModel(messages, "model-1", "");

		expect(result).toHaveLength(1);
		expect(result[0].role).toBe("user");
		expect(Array.isArray(result[0].content)).toBe(true);
		expect((result[0].content[0] as { type: string }).type).toBe("image_url");
	});

	it("includes assistant messages for target model", () => {
		const messages: ChatMessage[] = [
			{
				role: "assistant",
				content: "I am a response",
				model: "target-model",
				timestamp: Date.now(),
			},
		];

		const result = getApiMessagesForModel(messages, "target-model", "");

		expect(result).toHaveLength(1);
		expect(result[0]).toEqual({
			role: "assistant",
			content: "I am a response",
		});
	});

	it("converts assistant messages from other models to user role", () => {
		const messages: ChatMessage[] = [
			{
				role: "assistant",
				content: "Response from other model",
				model: "other-model",
				timestamp: Date.now(),
			},
		];

		const result = getApiMessagesForModel(messages, "target-model", "");

		expect(result).toHaveLength(1);
		expect(result[0]).toEqual({
			role: "user",
			content: "Response from other model",
		});
	});

	it("handles mixed conversation with multiple messages", () => {
		const messages: ChatMessage[] = [
			{
				role: "user",
				content: "Hello",
				model: "model-1",
				timestamp: Date.now(),
			},
			{
				role: "assistant",
				content: "Hi there",
				model: "target-model",
				timestamp: Date.now(),
			},
			{
				role: "user",
				content: "How are you?",
				model: "model-1",
				timestamp: Date.now(),
			},
		];

		const result = getApiMessagesForModel(messages, "target-model", "");

		expect(result).toHaveLength(3);
		expect(result[0].role).toBe("user");
		expect(result[1].role).toBe("assistant");
		expect(result[2].role).toBe("user");
	});

	it("handles conversation with persona and mixed messages", () => {
		const messages: ChatMessage[] = [
			{
				role: "user",
				content: "Hello",
				model: "model-1",
				timestamp: Date.now(),
			},
			{
				role: "assistant",
				content: "Response from other",
				model: "other-model",
				timestamp: Date.now(),
			},
		];

		const result = getApiMessagesForModel(
			messages,
			"target-model",
			"You are helpful",
		);

		expect(result).toHaveLength(3);
		expect(result[0].role).toBe("system");
		expect(result[1].role).toBe("user");
		expect(result[2].role).toBe("user");
	});
});

describe("streamModelResponse", () => {
	const baseMessages = [{ role: "user" as const, content: "hi" }];
	const baseParams = {};

	it("streams content from SSE response", async () => {
		const chunk1 = { choices: [{ delta: { content: "Hello" } }] };
		const chunk2 = { choices: [{ delta: { content: " World" } }] };

		server.use(...mockChatStream([chunk1, chunk2]));

		const result = await streamModelResponse(
			"model-1",
			baseMessages,
			baseParams,
			new AbortController(),
			vi.fn(),
		);

		expect(result.error).toBeNull();
		expect(result.content).toContain("Hello");
		expect(result.content).toContain("World");
		expect(result.rawContent).toContain("Hello");
		expect(result.rawContent).toContain("World");
	});

	it("extracts thinking content from reasoning_content", async () => {
		const chunk1 = {
			choices: [{ delta: { reasoning_content: "Let me think..." } }],
		};
		const chunk2 = {
			choices: [{ delta: { reasoning_content: " about this" } }],
		};

		server.use(...mockChatStream([chunk1, chunk2]));

		const result = await streamModelResponse(
			"model-1",
			baseMessages,
			baseParams,
			new AbortController(),
			vi.fn(),
		);

		expect(result.error).toBeNull();
		expect(result.thinkingContent).toContain("Let me think...");
		expect(result.thinkingContent).toContain(" about this");
	});

	it("extracts thinking content from reasoning field", async () => {
		const chunk1 = {
			choices: [{ delta: { reasoning: "First, I need to" } }],
		};
		const chunk2 = {
			choices: [{ delta: { reasoning: " analyze the problem" } }],
		};

		server.use(...mockChatStream([chunk1, chunk2]));

		const result = await streamModelResponse(
			"model-1",
			baseMessages,
			baseParams,
			new AbortController(),
			vi.fn(),
		);

		expect(result.error).toBeNull();
		expect(result.thinkingContent).toContain("First, I need to");
		expect(result.thinkingContent).toContain(" analyze the problem");
	});

	it("tracks usage tokens", async () => {
		const contentChunk = { choices: [{ delta: { content: "Hi" } }] };
		const usageChunk = { usage: { prompt_tokens: 50, completion_tokens: 100 } };

		server.use(...mockChatStream([contentChunk, usageChunk]));

		const result = await streamModelResponse(
			"model-1",
			baseMessages,
			baseParams,
			new AbortController(),
			vi.fn(),
		);

		expect(result.error).toBeNull();
		expect(result.promptTokens).toBe(50);
		expect(result.completionTokens).toBe(100);
	});

	it("computes tokensPerSecond", async () => {
		const contentChunk = { choices: [{ delta: { content: "Test" } }] };
		const usageChunk = { usage: { prompt_tokens: 10, completion_tokens: 20 } };

		server.use(...mockChatStream([contentChunk, usageChunk]));

		const result = await streamModelResponse(
			"model-1",
			baseMessages,
			baseParams,
			new AbortController(),
			vi.fn(),
		);

		expect(result.error).toBeNull();
		expect(result.tokensPerSecond).toBeGreaterThan(0);
		expect(typeof result.tokensPerSecond).toBe("number");
	});

	it("calls onDelta callback for each chunk", async () => {
		const chunk1 = { choices: [{ delta: { content: "A" } }] };
		const chunk2 = { choices: [{ delta: { content: "B" } }] };
		const onDelta = vi.fn();

		server.use(...mockChatStream([chunk1, chunk2]));

		await streamModelResponse(
			"model-1",
			baseMessages,
			baseParams,
			new AbortController(),
			onDelta,
		);

		expect(onDelta).toHaveBeenCalledTimes(2);
	});

	it("handles HTTP error response", async () => {
		server.use(...mockChatStream([], { status: 429 }));

		const result = await streamModelResponse(
			"model-1",
			baseMessages,
			baseParams,
			new AbortController(),
			vi.fn(),
		);

		expect(result.error).not.toBeNull();
		expect(result.error).toMatch(/429|Chat failed/);
	});

	it("handles stream without [DONE] sentinel and no content", async () => {
		server.use(...mockChatStream([], { doneSentinel: null }));

		const result = await streamModelResponse(
			"model-1",
			baseMessages,
			baseParams,
			new AbortController(),
			vi.fn(),
		);

		expect(result.error).not.toBeNull();
		expect(result.error).toContain("Stream ended unexpectedly");
	});

	it("handles stream without [DONE] but with content", async () => {
		const chunk1 = { choices: [{ delta: { content: "Content" } }] };
		server.use(...mockChatStream([chunk1], { doneSentinel: null }));

		const result = await streamModelResponse(
			"model-1",
			baseMessages,
			baseParams,
			new AbortController(),
			vi.fn(),
		);

		expect(result.error).not.toBeNull();
		expect(result.error).toContain("Stream ended without completion signal");
		expect(result.content).toContain("Content");
	});

	it("handles abort during streaming", async () => {
		const abortCtrl = new AbortController();
		const chunk1 = { choices: [{ delta: { content: "Start" } }] };
		const chunk2 = { choices: [{ delta: { content: "More" } }] };
		const chunk3 = { choices: [{ delta: { content: "Content" } }] };

		server.use(...mockChatStream([chunk1, chunk2, chunk3], { delay: 50 }));

		abortCtrl.abort();

		const result = await streamModelResponse(
			"model-1",
			baseMessages,
			baseParams,
			abortCtrl,
			vi.fn(),
		);

		expect(result.error).not.toBeNull();
		expect(result.error).toMatch(/abort|aborted/i);
	});
});
