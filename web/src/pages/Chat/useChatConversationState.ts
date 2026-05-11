import { useRef, useState } from "react";
import type { GenerationParams } from "../../api/types";
import { useLocalStorage } from "../../hooks/useLocalStorage";
import type { ConversationState } from "./chatStreaming";

interface UseChatConversationStateOptions {
	persistConversation: boolean;
}

export function useChatConversationState({
	persistConversation,
}: UseChatConversationStateOptions) {
	// ── Conversation mode state (Model A) ──
	const [conversationModelA, setConversationModelA] = useLocalStorage<string>(
		"conversationModelA",
		"",
		{ enabled: persistConversation },
	);
	const [conversationSystemPromptA, setConversationSystemPromptA] =
		useLocalStorage<string>("conversationSystemPromptA", "", {
			enabled: persistConversation,
		});
	const [conversationActivePersonaIdA, setConversationActivePersonaIdA] =
		useLocalStorage<string | null>("conversationActivePersonaIdA", null, {
			enabled: persistConversation,
			serialize: (v) => v ?? "",
			deserialize: (v) => v || null,
		});
	const [conversationParamsA, setConversationParamsA] =
		useState<GenerationParams>({});

	// ── Conversation mode state (Model B) ──
	const [selectedModelB, setSelectedModelB] = useLocalStorage<string>(
		"conversationModelB",
		"",
		{ enabled: persistConversation },
	);
	const [systemPromptB, setSystemPromptB] = useLocalStorage<string>(
		"conversationSystemPromptB",
		"",
		{ enabled: persistConversation },
	);
	const [activePersonaIdB, setActivePersonaIdB] = useLocalStorage<
		string | null
	>("conversationActivePersonaIdB", null, {
		enabled: persistConversation,
		serialize: (v) => v ?? "",
		deserialize: (v) => v || null,
	});
	const [messageParamsB, setMessageParamsB] = useLocalStorage<GenerationParams>(
		"conversationParamsB",
		{},
		{
			enabled: persistConversation,
			serialize: JSON.stringify,
			deserialize: (v) => JSON.parse(v),
		},
	);
	const [conversationState, setConversationState] =
		useState<ConversationState>("idle");
	const [currentTurn, setCurrentTurn] = useState(0);
	const [turnCountdown, setTurnCountdown] = useState(0);

	// ── Conversation config ──
	const [maxTurns, setMaxTurns] = useLocalStorage<number>(
		"conversationMaxTurns",
		10,
		{ serialize: String, deserialize: (v) => parseInt(v, 10) || 10 },
	);
	const [turnDelayMs, setTurnDelayMs] = useLocalStorage<number>(
		"conversationTurnDelayMs",
		500,
		{ serialize: String, deserialize: (v) => parseInt(v, 10) || 500 },
	);
	const [configCollapsed, setConfigCollapsed] = useState(false);

	// ── Conversation refs ──
	const conversationAbortRef = useRef<AbortController | null>(null);
	const conversationRunningRef = useRef(false);
	const capturedModelARef = useRef<string>("");
	const capturedModelBRef = useRef<string>("");

	return {
		conversationModelA,
		setConversationModelA,
		conversationSystemPromptA,
		setConversationSystemPromptA,
		conversationActivePersonaIdA,
		setConversationActivePersonaIdA,
		conversationParamsA,
		setConversationParamsA,
		selectedModelB,
		setSelectedModelB,
		systemPromptB,
		setSystemPromptB,
		activePersonaIdB,
		setActivePersonaIdB,
		messageParamsB,
		setMessageParamsB,
		conversationState,
		setConversationState,
		currentTurn,
		setCurrentTurn,
		turnCountdown,
		setTurnCountdown,
		maxTurns,
		setMaxTurns,
		turnDelayMs,
		setTurnDelayMs,
		configCollapsed,
		setConfigCollapsed,
		conversationAbortRef,
		conversationRunningRef,
		capturedModelARef,
		capturedModelBRef,
	};
}
