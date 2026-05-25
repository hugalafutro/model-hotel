import { renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useChatPersistence } from "../useChatPersistence";

// ── Mock functions ──
const toastMock = vi.fn();

// ── Mock all dependencies ──
vi.mock("../../../context/ToastContext", () => ({
	useToast: () => ({
		toast: toastMock,
		position: "bottom-right" as const,
		setPosition: vi.fn(),
		timeout: 3000,
		setTimeout: vi.fn(),
	}),
}));

function createQuotaExceededStorage() {
	return {
		getItem: vi.fn(() => null),
		setItem: vi.fn(() => {
			throw new DOMException("Quota exceeded", "QuotaExceededError");
		}),
		removeItem: vi.fn(),
		clear: vi.fn(),
		length: 0,
		key: vi.fn(() => null),
	};
}

describe("useChatPersistence", () => {
	beforeEach(() => {
		localStorage.clear();
		vi.clearAllMocks();
		toastMock.mockClear();
	});

	afterEach(() => {
		vi.unstubAllGlobals();
	});

	it("persists chat messages to localStorage 'chatMessages' key when persistChat=true", () => {
		const messages = [
			{ role: "user" as const, content: "Hello", timestamp: 1234567890 },
			{
				role: "assistant" as const,
				content: "Hi there",
				timestamp: 1234567891,
			},
		];

		renderHook(() =>
			useChatPersistence({
				messages,
				chatSubMode: "chat",
				persistChat: true,
				persistConversation: false,
			}),
		);

		const stored = localStorage.getItem("chatMessages");
		expect(stored).toBe(JSON.stringify(messages));
	});

	it("does NOT persist when persistChat=false", () => {
		const messages = [
			{ role: "user" as const, content: "Hello", timestamp: 1234567890 },
		];

		renderHook(() =>
			useChatPersistence({
				messages,
				chatSubMode: "chat",
				persistChat: false,
				persistConversation: false,
			}),
		);

		expect(localStorage.getItem("chatMessages")).toBeNull();
	});

	it("persists conversation messages to localStorage 'conversationMessages' key when persistConversation=true and chatSubMode='conversation'", () => {
		const messages = [
			{ role: "user" as const, content: "Hello", timestamp: 1234567890 },
			{
				role: "assistant" as const,
				content: "Hi there",
				timestamp: 1234567891,
			},
		];

		renderHook(() =>
			useChatPersistence({
				messages,
				chatSubMode: "conversation",
				persistChat: false,
				persistConversation: true,
			}),
		);

		const stored = localStorage.getItem("conversationMessages");
		expect(stored).toBe(JSON.stringify(messages));
	});

	it("does NOT persist conversation when chatSubMode is not 'conversation'", () => {
		const messages = [
			{ role: "user" as const, content: "Hello", timestamp: 1234567890 },
		];

		renderHook(() =>
			useChatPersistence({
				messages,
				chatSubMode: "chat",
				persistChat: false,
				persistConversation: true,
			}),
		);

		expect(localStorage.getItem("conversationMessages")).toBeNull();
	});

	it("handles localStorage quota exceeded gracefully", () => {
		const messages = [
			{ role: "user" as const, content: "Hello", timestamp: 1234567890 },
		];

		vi.stubGlobal("localStorage", createQuotaExceededStorage());

		// Should not throw even when localStorage is full
		expect(() =>
			renderHook(() =>
				useChatPersistence({
					messages,
					chatSubMode: "chat",
					persistChat: true,
					persistConversation: false,
				}),
			),
		).not.toThrow();
	});

	it("shows warning toast when localStorage quota exceeded in chat mode", () => {
		const messages = [
			{ role: "user" as const, content: "Hello", timestamp: 1234567890 },
		];

		vi.stubGlobal("localStorage", createQuotaExceededStorage());

		renderHook(() =>
			useChatPersistence({
				messages,
				chatSubMode: "chat",
				persistChat: true,
				persistConversation: false,
			}),
		);

		// Verify warning toast was called
		expect(toastMock).toHaveBeenCalledWith(
			"Storage full - chat history not saved",
			"warning",
		);
	});

	it("shows warning toast when localStorage quota exceeded in conversation mode", () => {
		const messages = [
			{ role: "user" as const, content: "Hello", timestamp: 1234567890 },
		];

		vi.stubGlobal("localStorage", createQuotaExceededStorage());

		renderHook(() =>
			useChatPersistence({
				messages,
				chatSubMode: "conversation",
				persistChat: false,
				persistConversation: true,
			}),
		);

		// Verify warning toast was called
		expect(toastMock).toHaveBeenCalledWith(
			"Storage full - chat history not saved",
			"warning",
		);
	});

	it("shows quota warning only once (quotaWarnedRef prevents repeated warnings)", () => {
		const messages1 = [
			{ role: "user" as const, content: "Hello", timestamp: 1234567890 },
		];
		const messages2 = [
			{ role: "user" as const, content: "World", timestamp: 1234567891 },
		];

		vi.stubGlobal("localStorage", createQuotaExceededStorage());

		// First render - should show warning
		const { rerender } = renderHook(
			(props) =>
				useChatPersistence({
					messages: props.messages,
					chatSubMode: "chat",
					persistChat: true,
					persistConversation: false,
				}),
			{ initialProps: { messages: messages1 } },
		);

		// Verify warning was shown once
		expect(toastMock).toHaveBeenCalledTimes(1);
		expect(toastMock).toHaveBeenCalledWith(
			"Storage full - chat history not saved",
			"warning",
		);

		// Rerender with different messages - should NOT show warning again
		rerender({ messages: messages2 });

		// Toast should still only have been called once
		expect(toastMock).toHaveBeenCalledTimes(1);
	});
});
