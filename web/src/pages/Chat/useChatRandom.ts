import i18next from "i18next";
import { useCallback } from "react";
import type { Model } from "../../api/types";
import { CHAT_PERSONAS } from "../../data/presets";
import { pickRandom, randomChatModelId } from "../../utils/random";

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
		const pick = pickRandom(CHAT_PERSONAS.filter((p) => p.id !== currentId));
		if (!pick) return;
		setActivePersonaId(pick.id);
		setSystemPrompt(i18next.t(pick.systemPrompt));
	}, [
		chatSubMode,
		chatActivePersonaId,
		conversationActivePersonaIdA,
		setActivePersonaId,
		setSystemPrompt,
	]);

	const handleRandomPersonaB = useCallback(() => {
		const pick = pickRandom(
			CHAT_PERSONAS.filter((p) => p.id !== activePersonaIdB),
		);
		if (!pick) return;
		setActivePersonaIdB(pick.id);
		setSystemPromptB(i18next.t(pick.systemPrompt));
	}, [activePersonaIdB, setActivePersonaIdB, setSystemPromptB]);

	const handleRandomModel = useCallback(() => {
		const val = randomChatModelId(enabledModels, [selectedModel]);
		if (val) setSelectedModel(val);
	}, [enabledModels, selectedModel, setSelectedModel]);

	const handleRandomModelB = useCallback(() => {
		const val = randomChatModelId(enabledModels, [selectedModelB]);
		if (val) setSelectedModelB(val);
	}, [enabledModels, selectedModelB, setSelectedModelB]);

	return {
		handleRandomPersona,
		handleRandomPersonaB,
		handleRandomModel,
		handleRandomModelB,
	};
}
