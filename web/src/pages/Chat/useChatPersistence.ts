import { useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import type { ChatMessage } from "../../api/types";
import type { ChatSubMode } from "../../context/SidebarModeContext";
import { useToast } from "../../context/ToastContext";

interface ChatPersistenceParams {
	messages: ChatMessage[];
	chatSubMode: ChatSubMode;
	persistChat: boolean;
	persistConversation: boolean;
}

export function useChatPersistence({
	messages,
	chatSubMode,
	persistChat,
	persistConversation,
}: ChatPersistenceParams) {
	const { toast } = useToast();
	const { t } = useTranslation();
	const quotaWarnedRef = useRef(false);

	// ── Chat mode persistence effect ──
	useEffect(() => {
		if (!persistChat) return;
		try {
			localStorage.setItem("chatMessages", JSON.stringify(messages));
		} catch {
			/* quota exceeded */
			if (!quotaWarnedRef.current) {
				quotaWarnedRef.current = true;
				toast(t("hooks.useChatPersistence.storageFullChat"), "warning");
			}
		}
	}, [messages, persistChat, t, toast]);

	// ── Conversation messages persistence effect ──
	useEffect(() => {
		if (!persistConversation) return;
		if (chatSubMode !== "conversation") return;
		try {
			localStorage.setItem("conversationMessages", JSON.stringify(messages));
		} catch {
			/* quota exceeded */
			if (!quotaWarnedRef.current) {
				quotaWarnedRef.current = true;
				toast(t("hooks.useChatPersistence.storageFullChat"), "warning");
			}
		}
	}, [messages, persistConversation, chatSubMode, t, toast]);
}
