import { act, renderHook } from "@testing-library/react";
import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ChatMessage } from "../../../api/types";
import type { StreamResult } from "../chatStreaming";
import { useAssistantStream } from "../useAssistantStream";

const streamMock = vi.hoisted(() => vi.fn());
vi.mock("../chatStreaming", async (orig) => ({
	...(await orig<typeof import("../chatStreaming")>()),
	streamModelResponse: streamMock,
}));

const abortedResult: StreamResult = {
	rawContent: "",
	content: "",
	thinkingContent: "",
	error: null,
	aborted: true,
	durationMs: 0,
	tokensPerSecond: null,
	promptTokens: 0,
	completionTokens: 0,
};

function useHarness() {
	const [messages, setMessages] = useState<ChatMessage[]>([]);
	const [input, setInput] = useState("hello");
	const [isStreaming, setIsStreaming] = useState(false);
	const stream = useAssistantStream({
		messages,
		setMessages,
		input,
		setInput,
		selectedModel: "m",
		systemPrompt: "",
		messageParams: {},
		modelsReady: true,
		isStreaming,
		setIsStreaming,
		pendingImage: null,
		setPendingImage: vi.fn(),
		pendingAudio: null,
		setPendingAudio: vi.fn(),
		toast: vi.fn(),
		t: ((k: string) => k) as never,
	});
	return { ...stream, isStreaming, setIsStreaming };
}

describe("useAssistantStream", () => {
	afterEach(() => {
		streamMock.mockReset();
	});

	it("a stopped reply settling late leaves a later stream's flag alone", async () => {
		let settle!: (r: StreamResult) => void;
		streamMock.mockReturnValueOnce(
			new Promise<StreamResult>((resolve) => {
				settle = resolve;
			}),
		);
		const { result } = renderHook(() => useHarness());

		let send!: Promise<void>;
		act(() => {
			send = result.current.handleSend();
		});
		expect(result.current.isStreaming).toBe(true);

		act(() => result.current.handleStop());
		// Another stream (a conversation after a sub-mode switch) takes the
		// shared flag while the stopped request has not settled yet.
		act(() => result.current.setIsStreaming(true));

		await act(async () => {
			settle(abortedResult);
			await send;
		});
		expect(result.current.isStreaming).toBe(true);
	});
});
