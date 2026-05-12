import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { StorageProvider, useStorage } from "../StorageContext";

describe("StorageContext", () => {
	it("useStorage returns default values when no localStorage", () => {
		const { result } = renderHook(() => useStorage(), {
			wrapper: StorageProvider,
		});

		expect(result.current.persistChat).toBe(false);
		expect(result.current.persistArena).toBe(false);
		expect(result.current.persistConversation).toBe(false);
		expect(result.current.arenaHistoryEnabled).toBe(false);
		expect(result.current.arenaHistoryLimit).toBe(25);
	});

	it("setPersistChat toggles the value", () => {
		const { result } = renderHook(() => useStorage(), {
			wrapper: StorageProvider,
		});

		expect(result.current.persistChat).toBe(false);

		act(() => {
			result.current.setPersistChat(true);
		});

		expect(result.current.persistChat).toBe(true);

		act(() => {
			result.current.setPersistChat(false);
		});

		expect(result.current.persistChat).toBe(false);
	});

	it("persistArena toggles", () => {
		const { result } = renderHook(() => useStorage(), {
			wrapper: StorageProvider,
		});

		expect(result.current.persistArena).toBe(false);

		act(() => {
			result.current.setPersistArena(true);
		});

		expect(result.current.persistArena).toBe(true);

		act(() => {
			result.current.setPersistArena(false);
		});

		expect(result.current.persistArena).toBe(false);
	});

	it("persistConversation toggles", () => {
		const { result } = renderHook(() => useStorage(), {
			wrapper: StorageProvider,
		});

		expect(result.current.persistConversation).toBe(false);

		act(() => {
			result.current.setPersistConversation(true);
		});

		expect(result.current.persistConversation).toBe(true);

		act(() => {
			result.current.setPersistConversation(false);
		});

		expect(result.current.persistConversation).toBe(false);
	});

	it("arenaHistoryEnabled toggles", () => {
		const { result } = renderHook(() => useStorage(), {
			wrapper: StorageProvider,
		});

		expect(result.current.arenaHistoryEnabled).toBe(false);

		act(() => {
			result.current.setArenaHistoryEnabled(true);
		});

		expect(result.current.arenaHistoryEnabled).toBe(true);

		act(() => {
			result.current.setArenaHistoryEnabled(false);
		});

		expect(result.current.arenaHistoryEnabled).toBe(false);
	});

	it("arenaHistoryLimit can be set to a number", () => {
		const { result } = renderHook(() => useStorage(), {
			wrapper: StorageProvider,
		});

		expect(result.current.arenaHistoryLimit).toBe(25);

		act(() => {
			result.current.setArenaHistoryLimit(50);
		});

		expect(result.current.arenaHistoryLimit).toBe(50);

		act(() => {
			result.current.setArenaHistoryLimit(10);
		});

		expect(result.current.arenaHistoryLimit).toBe(10);
	});

	it("Setting persistChat to false clears chatMessages, chatSystemPrompt, chatActivePersonaId from localStorage", () => {
		// Set up localStorage with chat data
		localStorage.setItem("chatMessages", "[]");
		localStorage.setItem("chatSystemPrompt", "test prompt");
		localStorage.setItem("chatActivePersonaId", "persona-1");

		const { result } = renderHook(() => useStorage(), {
			wrapper: StorageProvider,
		});

		// First enable persistChat
		act(() => {
			result.current.setPersistChat(true);
		});

		expect(localStorage.getItem("chatMessages")).toBe("[]");

		// Now disable - should clear the keys
		act(() => {
			result.current.setPersistChat(false);
		});

		expect(localStorage.getItem("chatMessages")).toBeNull();
		expect(localStorage.getItem("chatSystemPrompt")).toBeNull();
		expect(localStorage.getItem("chatActivePersonaId")).toBeNull();
	});

	it("Setting persistArena to false clears arena-related localStorage keys", () => {
		localStorage.setItem("arenaCompetitionPrompt", "comp prompt");
		localStorage.setItem("arenaComparePrompt", "compare prompt");
		localStorage.setItem("arenaCompetitionActivePromptId", "p1");
		localStorage.setItem("arenaCompareActivePromptId", "p2");
		localStorage.setItem("arenaState", "{}");

		const { result } = renderHook(() => useStorage(), {
			wrapper: StorageProvider,
		});

		act(() => {
			result.current.setPersistArena(false);
		});

		expect(localStorage.getItem("arenaCompetitionPrompt")).toBeNull();
		expect(localStorage.getItem("arenaComparePrompt")).toBeNull();
		expect(localStorage.getItem("arenaCompetitionActivePromptId")).toBeNull();
		expect(localStorage.getItem("arenaCompareActivePromptId")).toBeNull();
		expect(localStorage.getItem("arenaState")).toBeNull();
	});

	it("Setting persistConversation to false clears conversation-related localStorage keys", () => {
		localStorage.setItem("conversationMessages", "[]");
		localStorage.setItem("conversationState", "{}");

		const { result } = renderHook(() => useStorage(), {
			wrapper: StorageProvider,
		});

		act(() => {
			result.current.setPersistConversation(false);
		});

		expect(localStorage.getItem("conversationMessages")).toBeNull();
		expect(localStorage.getItem("conversationState")).toBeNull();
	});

	it("Setting arenaHistoryEnabled to false clears arenaMatchHistory", () => {
		localStorage.setItem("arenaMatchHistory", "[]");

		const { result } = renderHook(() => useStorage(), {
			wrapper: StorageProvider,
		});

		act(() => {
			result.current.setArenaHistoryEnabled(false);
		});

		expect(localStorage.getItem("arenaMatchHistory")).toBeNull();
	});
});
