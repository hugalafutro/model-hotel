import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { useChatConversationState } from "../useChatConversationState";

const createWrapper = () => {
	return function Wrapper({ children }: { children: React.ReactNode }) {
		return <>{children}</>;
	};
};

describe("useChatConversationState", () => {
	it("returns initial state with defaults", () => {
		const { result } = renderHook(
			() => useChatConversationState({ persistConversation: false }),
			{ wrapper: createWrapper() },
		);

		expect(result.current.conversationModelA).toBe("");
		expect(result.current.conversationSystemPromptA).toBe("");
		expect(result.current.conversationActivePersonaIdA).toBe(null);
		expect(result.current.conversationParamsA).toEqual({});
		expect(result.current.selectedModelB).toBe("");
		expect(result.current.systemPromptB).toBe("");
		expect(result.current.activePersonaIdB).toBe(null);
		expect(result.current.messageParamsB).toEqual({});
		expect(result.current.conversationState).toBe("idle");
		expect(result.current.currentTurn).toBe(0);
		expect(result.current.turnCountdown).toBe(0);
		expect(result.current.maxTurns).toBe(10);
		expect(result.current.turnDelayMs).toBe(500);
		expect(result.current.configCollapsed).toBe(false);
	});

	it("provides setter functions for all state values", () => {
		const { result } = renderHook(
			() => useChatConversationState({ persistConversation: false }),
			{ wrapper: createWrapper() },
		);

		expect(typeof result.current.setConversationModelA).toBe("function");
		expect(typeof result.current.setConversationSystemPromptA).toBe("function");
		expect(typeof result.current.setConversationActivePersonaIdA).toBe(
			"function",
		);
		expect(typeof result.current.setConversationParamsA).toBe("function");
		expect(typeof result.current.setSelectedModelB).toBe("function");
		expect(typeof result.current.setSystemPromptB).toBe("function");
		expect(typeof result.current.setActivePersonaIdB).toBe("function");
		expect(typeof result.current.setMessageParamsB).toBe("function");
		expect(typeof result.current.setConversationState).toBe("function");
		expect(typeof result.current.setCurrentTurn).toBe("function");
		expect(typeof result.current.setTurnCountdown).toBe("function");
		expect(typeof result.current.setMaxTurns).toBe("function");
		expect(typeof result.current.setTurnDelayMs).toBe("function");
		expect(typeof result.current.setConfigCollapsed).toBe("function");
	});

	it("provides refs for conversation control", () => {
		const { result } = renderHook(
			() => useChatConversationState({ persistConversation: false }),
			{ wrapper: createWrapper() },
		);

		expect(result.current.conversationAbortRef).toBeDefined();
		expect(result.current.conversationRunningRef).toBeDefined();
		expect(result.current.capturedModelARef).toBeDefined();
		expect(result.current.capturedModelBRef).toBeDefined();
		expect(result.current.conversationAbortRef.current).toBe(null);
		expect(result.current.conversationRunningRef.current).toBe(false);
		expect(result.current.capturedModelARef.current).toBe("");
		expect(result.current.capturedModelBRef.current).toBe("");
	});

	it("updates conversationModelA via setter", () => {
		const { result } = renderHook(
			() => useChatConversationState({ persistConversation: false }),
			{ wrapper: createWrapper() },
		);

		act(() => {
			result.current.setConversationModelA("gpt-4");
		});

		expect(result.current.conversationModelA).toBe("gpt-4");
	});

	it("updates conversationSystemPromptA via setter", () => {
		const { result } = renderHook(
			() => useChatConversationState({ persistConversation: false }),
			{ wrapper: createWrapper() },
		);

		act(() => {
			result.current.setConversationSystemPromptA(
				"You are a helpful assistant",
			);
		});

		expect(result.current.conversationSystemPromptA).toBe(
			"You are a helpful assistant",
		);
	});

	it("updates conversationActivePersonaIdA via setter", () => {
		const { result } = renderHook(
			() => useChatConversationState({ persistConversation: false }),
			{ wrapper: createWrapper() },
		);

		act(() => {
			result.current.setConversationActivePersonaIdA("persona-123");
		});

		expect(result.current.conversationActivePersonaIdA).toBe("persona-123");

		act(() => {
			result.current.setConversationActivePersonaIdA(null);
		});

		expect(result.current.conversationActivePersonaIdA).toBe(null);
	});

	it("updates conversationParamsA via setter", () => {
		const { result } = renderHook(
			() => useChatConversationState({ persistConversation: false }),
			{ wrapper: createWrapper() },
		);

		act(() => {
			result.current.setConversationParamsA({
				temperature: 0.8,
				max_tokens: 2048,
			});
		});

		expect(result.current.conversationParamsA).toEqual({
			temperature: 0.8,
			max_tokens: 2048,
		});
	});

	it("updates selectedModelB via setter", () => {
		const { result } = renderHook(
			() => useChatConversationState({ persistConversation: false }),
			{ wrapper: createWrapper() },
		);

		act(() => {
			result.current.setSelectedModelB("claude-3");
		});

		expect(result.current.selectedModelB).toBe("claude-3");
	});

	it("updates systemPromptB via setter", () => {
		const { result } = renderHook(
			() => useChatConversationState({ persistConversation: false }),
			{ wrapper: createWrapper() },
		);

		act(() => {
			result.current.setSystemPromptB("You are a coding assistant");
		});

		expect(result.current.systemPromptB).toBe("You are a coding assistant");
	});

	it("updates activePersonaIdB via setter", () => {
		const { result } = renderHook(
			() => useChatConversationState({ persistConversation: false }),
			{ wrapper: createWrapper() },
		);

		act(() => {
			result.current.setActivePersonaIdB("persona-456");
		});

		expect(result.current.activePersonaIdB).toBe("persona-456");

		act(() => {
			result.current.setActivePersonaIdB(null);
		});

		expect(result.current.activePersonaIdB).toBe(null);
	});

	it("updates messageParamsB via setter", () => {
		const { result } = renderHook(
			() => useChatConversationState({ persistConversation: false }),
			{ wrapper: createWrapper() },
		);

		act(() => {
			result.current.setMessageParamsB({ temperature: 0.5, top_p: 0.9 });
		});

		expect(result.current.messageParamsB).toEqual({
			temperature: 0.5,
			top_p: 0.9,
		});
	});

	it("updates conversationState via setter", () => {
		const { result } = renderHook(
			() => useChatConversationState({ persistConversation: false }),
			{ wrapper: createWrapper() },
		);

		act(() => {
			result.current.setConversationState("running");
		});

		expect(result.current.conversationState).toBe("running");

		act(() => {
			result.current.setConversationState("paused");
		});

		expect(result.current.conversationState).toBe("paused");
	});

	it("updates currentTurn via setter", () => {
		const { result } = renderHook(
			() => useChatConversationState({ persistConversation: false }),
			{ wrapper: createWrapper() },
		);

		act(() => {
			result.current.setCurrentTurn(5);
		});

		expect(result.current.currentTurn).toBe(5);
	});

	it("updates turnCountdown via setter", () => {
		const { result } = renderHook(
			() => useChatConversationState({ persistConversation: false }),
			{ wrapper: createWrapper() },
		);

		act(() => {
			result.current.setTurnCountdown(10);
		});

		expect(result.current.turnCountdown).toBe(10);
	});

	it("updates maxTurns via setter", () => {
		const { result } = renderHook(
			() => useChatConversationState({ persistConversation: false }),
			{ wrapper: createWrapper() },
		);

		act(() => {
			result.current.setMaxTurns(20);
		});

		expect(result.current.maxTurns).toBe(20);
	});

	it("updates turnDelayMs via setter", () => {
		const { result } = renderHook(
			() => useChatConversationState({ persistConversation: false }),
			{ wrapper: createWrapper() },
		);

		act(() => {
			result.current.setTurnDelayMs(1000);
		});

		expect(result.current.turnDelayMs).toBe(1000);
	});

	it("updates configCollapsed via setter", () => {
		const { result } = renderHook(
			() => useChatConversationState({ persistConversation: false }),
			{ wrapper: createWrapper() },
		);

		act(() => {
			result.current.setConfigCollapsed(true);
		});

		expect(result.current.configCollapsed).toBe(true);
	});

	it("can update refs directly", () => {
		const { result } = renderHook(
			() => useChatConversationState({ persistConversation: false }),
			{ wrapper: createWrapper() },
		);

		act(() => {
			result.current.conversationAbortRef.current = new AbortController();
		});

		expect(result.current.conversationAbortRef.current).toBeInstanceOf(
			AbortController,
		);

		act(() => {
			result.current.conversationRunningRef.current = true;
		});

		expect(result.current.conversationRunningRef.current).toBe(true);

		act(() => {
			result.current.capturedModelARef.current = "model-a-123";
		});

		expect(result.current.capturedModelARef.current).toBe("model-a-123");

		act(() => {
			result.current.capturedModelBRef.current = "model-b-456";
		});

		expect(result.current.capturedModelBRef.current).toBe("model-b-456");
	});

	it("persists state to localStorage when persistConversation is true", () => {
		const { result: result1 } = renderHook(
			() => useChatConversationState({ persistConversation: true }),
			{ wrapper: createWrapper() },
		);

		act(() => {
			result1.current.setConversationModelA("persisted-model");
		});

		expect(localStorage.getItem("conversationModelA")).toBe("persisted-model");

		const { result: result2 } = renderHook(
			() => useChatConversationState({ persistConversation: true }),
			{ wrapper: createWrapper() },
		);

		expect(result2.current.conversationModelA).toBe("persisted-model");
	});

	it("does not persist state when persistConversation is false", () => {
		localStorage.clear();

		const { result } = renderHook(
			() => useChatConversationState({ persistConversation: false }),
			{ wrapper: createWrapper() },
		);

		act(() => {
			result.current.setConversationModelA("non-persisted-model");
		});

		expect(localStorage.getItem("conversationModelA")).toBe(null);
	});

	it("serializes and deserializes conversationActivePersonaIdA with null values", () => {
		localStorage.clear();

		// Set to null - should serialize to empty string
		const { result: result1 } = renderHook(
			() => useChatConversationState({ persistConversation: true }),
			{ wrapper: createWrapper() },
		);

		act(() => {
			result1.current.setConversationActivePersonaIdA(null);
		});

		// Verify it serializes to empty string
		expect(localStorage.getItem("conversationActivePersonaIdA")).toBe("");

		// Verify it deserializes back to null
		expect(result1.current.conversationActivePersonaIdA).toBe(null);

		// Test round-trip: create new hook instance to verify deserialization
		const { result: result2 } = renderHook(
			() => useChatConversationState({ persistConversation: true }),
			{ wrapper: createWrapper() },
		);

		expect(result2.current.conversationActivePersonaIdA).toBe(null);

		// Test with non-null value
		act(() => {
			result2.current.setConversationActivePersonaIdA("persona-123");
		});

		expect(localStorage.getItem("conversationActivePersonaIdA")).toBe(
			"persona-123",
		);
		expect(result2.current.conversationActivePersonaIdA).toBe("persona-123");
	});

	it("serializes and deserializes activePersonaIdB with null values", () => {
		localStorage.clear();

		// Set to null - should serialize to empty string
		const { result: result1 } = renderHook(
			() => useChatConversationState({ persistConversation: true }),
			{ wrapper: createWrapper() },
		);

		act(() => {
			result1.current.setActivePersonaIdB(null);
		});

		// Verify it serializes to empty string
		expect(localStorage.getItem("conversationActivePersonaIdB")).toBe("");

		// Verify it deserializes back to null
		expect(result1.current.activePersonaIdB).toBe(null);

		// Test round-trip: create new hook instance to verify deserialization
		const { result: result2 } = renderHook(
			() => useChatConversationState({ persistConversation: true }),
			{ wrapper: createWrapper() },
		);

		expect(result2.current.activePersonaIdB).toBe(null);

		// Test with non-null value
		act(() => {
			result2.current.setActivePersonaIdB("persona-456");
		});

		expect(localStorage.getItem("conversationActivePersonaIdB")).toBe(
			"persona-456",
		);
		expect(result2.current.activePersonaIdB).toBe("persona-456");
	});

	it("serializes and deserializes messageParamsB with JSON.stringify/parse", () => {
		localStorage.clear();

		const params = { temperature: 0.5, top_p: 0.9, max_tokens: 2048 };

		// Set params - should serialize via JSON.stringify
		const { result: result1 } = renderHook(
			() => useChatConversationState({ persistConversation: true }),
			{ wrapper: createWrapper() },
		);

		act(() => {
			result1.current.setMessageParamsB(params);
		});

		// Verify it serializes to JSON string
		const stored = localStorage.getItem("conversationParamsB");
		expect(stored).toBe(JSON.stringify(params));

		// Verify it deserializes back to same object via JSON.parse
		expect(result1.current.messageParamsB).toEqual(params);

		// Test round-trip: create new hook instance to verify deserialization
		const { result: result2 } = renderHook(
			() => useChatConversationState({ persistConversation: true }),
			{ wrapper: createWrapper() },
		);

		expect(result2.current.messageParamsB).toEqual(params);

		// Test with different params
		const newParams = { temperature: 0.8 };
		act(() => {
			result2.current.setMessageParamsB(newParams);
		});

		expect(localStorage.getItem("conversationParamsB")).toBe(
			JSON.stringify(newParams),
		);
		expect(result2.current.messageParamsB).toEqual(newParams);
	});

	it("falls back to default maxTurns when localStorage has invalid value", () => {
		localStorage.clear();
		localStorage.setItem("conversationMaxTurns", "abc");

		const { result } = renderHook(
			() => useChatConversationState({ persistConversation: true }),
			{ wrapper: createWrapper() },
		);

		expect(result.current.maxTurns).toBe(10);
	});

	it("falls back to default turnDelayMs when localStorage has invalid value", () => {
		localStorage.clear();
		localStorage.setItem("conversationTurnDelayMs", "invalid");

		const { result } = renderHook(
			() => useChatConversationState({ persistConversation: true }),
			{ wrapper: createWrapper() },
		);

		expect(result.current.turnDelayMs).toBe(500);
	});
});
