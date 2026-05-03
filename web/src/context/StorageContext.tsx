import { createContext, type ReactNode, useContext } from "react";
import { useLocalStorage } from "../hooks/useLocalStorage";

interface StorageContextType {
	persistChat: boolean;
	setPersistChat: (v: boolean) => void;
	persistArena: boolean;
	setPersistArena: (v: boolean) => void;
	persistConversation: boolean;
	setPersistConversation: (v: boolean) => void;
	arenaHistoryEnabled: boolean;
	setArenaHistoryEnabled: (v: boolean) => void;
	arenaHistoryLimit: number;
	setArenaHistoryLimit: (n: number) => void;
}

const StorageContext = createContext<StorageContextType>({
	persistChat: false,
	setPersistChat: () => {},
	persistArena: false,
	setPersistArena: () => {},
	persistConversation: false,
	setPersistConversation: () => {},
	arenaHistoryEnabled: false,
	setArenaHistoryEnabled: () => {},
	arenaHistoryLimit: 25,
	setArenaHistoryLimit: () => {},
});

// eslint-disable-next-line react-refresh/only-export-components
export function useStorage() {
	return useContext(StorageContext);
}

export function StorageProvider({ children }: { children: ReactNode }) {
	const [persistChat, setPersistChatRaw] = useLocalStorage<boolean>(
		"persistChat",
		false,
		{ deserialize: (v) => v === "true" },
	);
	const [persistArena, setPersistArenaRaw] = useLocalStorage<boolean>(
		"persistArena",
		false,
		{ deserialize: (v) => v === "true" },
	);
	const [persistConversation, setPersistConversationRaw] =
		useLocalStorage<boolean>("persistConversation", false, {
			deserialize: (v) => v === "true",
		});
	const [arenaHistoryEnabled, setArenaHistoryEnabledRaw] =
		useLocalStorage<boolean>("arenaHistoryEnabled", false, {
			deserialize: (v) => v === "true",
		});
	const [arenaHistoryLimit, setArenaHistoryLimitRaw] = useLocalStorage<number>(
		"arenaHistoryLimit",
		25,
		{
			serialize: String,
			deserialize: (v) => {
				const parsed = parseInt(v, 10);
				return !Number.isNaN(parsed) && parsed > 0 ? parsed : 25;
			},
		},
	);

	const setPersistChat = (v: boolean) => {
		setPersistChatRaw(v);
		if (!v) {
			localStorage.removeItem("chatMessages");
			localStorage.removeItem("chatSystemPrompt");
			localStorage.removeItem("chatActivePersonaId");
		}
	};

	const setPersistArena = (v: boolean) => {
		setPersistArenaRaw(v);
		if (!v) {
			localStorage.removeItem("arenaCompetitionPrompt");
			localStorage.removeItem("arenaComparePrompt");
			localStorage.removeItem("arenaCompetitionActivePromptId");
			localStorage.removeItem("arenaCompareActivePromptId");
			localStorage.removeItem("arenaState");
		}
	};

	const setPersistConversation = (v: boolean) => {
		setPersistConversationRaw(v);
		if (!v) {
			localStorage.removeItem("conversationMessages");
			localStorage.removeItem("conversationState");
		}
	};

	const setArenaHistoryEnabled = (v: boolean) => {
		setArenaHistoryEnabledRaw(v);
		if (!v) {
			localStorage.removeItem("arenaMatchHistory");
		}
	};

	const setArenaHistoryLimit = (n: number) => {
		setArenaHistoryLimitRaw(n);
	};

	return (
		<StorageContext.Provider
			value={{
				persistChat,
				setPersistChat,
				persistArena,
				setPersistArena,
				persistConversation,
				setPersistConversation,
				arenaHistoryEnabled,
				setArenaHistoryEnabled,
				arenaHistoryLimit,
				setArenaHistoryLimit,
			}}
		>
			{children}
		</StorageContext.Provider>
	);
}
