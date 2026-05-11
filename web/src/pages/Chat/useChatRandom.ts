import { useCallback } from "react";
import type { Model } from "../../api/types";
import { CHAT_PERSONAS } from "../../data/presets";
import { proxyModelID } from "../../utils/model";

interface UseChatRandomActionsParams {
	chatSubMode: "chat" | "conversation";
	chatActivePersonaId: string | null;
	conversationActivePersonaIdA: string | null;
	activePersonaIdB: string | null;
	selectedModel: string;
	selectedModelB: string;
	enabledModels: Model[];
	setActivePersonaId: (id: string | null) => void;
	setSystemPrompt: (prompt: string) => void;
	setActivePersonaIdB: (id: string | null) => void;
	setSystemPromptB: (prompt: string) => void;
	setSelectedModel: (model: string) => void;
	setSelectedModelB: (model: string) => void;
}

export function useChatRandomActions({
	chatSubMode,
	chatActivePersonaId,
	conversationActivePersonaIdA,
	activePersonaIdB,
	selectedModel,
	selectedModelB,
	enabledModels,
	setActivePersonaId,
	setSystemPrompt,
	setActivePersonaIdB,
	setSystemPromptB,
	setSelectedModel,
	setSelectedModelB,
}: UseChatRandomActionsParams) {
	const handleRandomPersona = useCallback(() => {
		const currentId =
			chatSubMode === "chat"
				? chatActivePersonaId
				: conversationActivePersonaIdA;
		const available = CHAT_PERSONAS.filter((p) => p.id !== currentId);
		if (available.length === 0) return;
		const pick = available[Math.floor(Math.random() * available.length)];
		setActivePersonaId(pick.id);
		setSystemPrompt(pick.systemPrompt);
	}, [
		chatSubMode,
		chatActivePersonaId,
		conversationActivePersonaIdA,
		setActivePersonaId,
		setSystemPrompt,
	]);

	const handleRandomPersonaB = useCallback(() => {
		const available = CHAT_PERSONAS.filter((p) => p.id !== activePersonaIdB);
		if (available.length === 0) return;
		const pick = available[Math.floor(Math.random() * available.length)];
		setActivePersonaIdB(pick.id);
		setSystemPromptB(pick.systemPrompt);
	}, [activePersonaIdB, setActivePersonaIdB, setSystemPromptB]);

	const handleRandomModel = useCallback(() => {
		const available = enabledModels.filter((m) => {
			const val = proxyModelID(m.provider_name, m.model_id);
			return val !== selectedModel;
		});
		if (available.length === 0) return;
		const pick = available[Math.floor(Math.random() * available.length)];
		const val = proxyModelID(pick.provider_name, pick.model_id);
		setSelectedModel(val);
	}, [enabledModels, selectedModel, setSelectedModel]);

	const handleRandomModelB = useCallback(() => {
		const available = enabledModels.filter((m) => {
			const val = proxyModelID(m.provider_name, m.model_id);
			return val !== selectedModelB;
		});
		if (available.length === 0) return;
		const pick = available[Math.floor(Math.random() * available.length)];
		const val = proxyModelID(pick.provider_name, pick.model_id);
		setSelectedModelB(val);
	}, [enabledModels, selectedModelB, setSelectedModelB]);

	return {
		handleRandomPersona,
		handleRandomPersonaB,
		handleRandomModel,
		handleRandomModelB,
	};
}
