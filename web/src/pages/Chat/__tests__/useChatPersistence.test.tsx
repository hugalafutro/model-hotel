import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useChatPersistence } from "../useChatPersistence";

vi.mock("../../../context/ToastContext", () => ({
	useToast: () => ({
		toast: vi.fn(),
		position: "bottom-right" as const,
		setPosition: vi.fn(),
		timeout: 3000,
		setTimeout: vi.fn(),
	}),
}));

describe("useChatPersistence", () => {
	beforeEach(() => {
		localStorage.clear();
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

		const setItemSpy = vi
			.spyOn(Storage.prototype, "setItem")
			.mockImplementation(() => {
				throw new Error("QuotaExceededError");
			});

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

		setItemSpy.mockRestore();
	});
});
