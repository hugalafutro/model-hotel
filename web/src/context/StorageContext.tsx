import { createContext, type ReactNode, useContext } from "react";
import { storedBool, useLocalStorage } from "../hooks/useLocalStorage";
import {
	ARENA_HISTORY_ENABLED_KEY,
	ARENA_HISTORY_KEY,
	ARENA_HISTORY_LIMIT_KEY,
	ARENA_STORAGE_KEYS,
	DEFAULT_ARENA_HISTORY_LIMIT,
	parseArenaHistoryLimit,
} from "../utils/arenaHistory";

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

// eslint-disable-next-line react-refresh/only-export-components -- the consumer hook lives beside its provider
export function useStorage() {
	return useContext(StorageContext);
}

export function StorageProvider({ children }: { children: ReactNode }) {
	const [persistChat, setPersistChatRaw] = useLocalStorage<boolean>(
		"persistChat",
		false,
		{ deserialize: storedBool },
	);
	const [persistArena, setPersistArenaRaw] = useLocalStorage<boolean>(
		"persistArena",
		false,
		{ deserialize: storedBool },
	);
	const [persistConversation, setPersistConversationRaw] =
		useLocalStorage<boolean>("persistConversation", false, {
			deserialize: storedBool,
		});
	const [arenaHistoryEnabled, setArenaHistoryEnabledRaw] =
		useLocalStorage<boolean>(ARENA_HISTORY_ENABLED_KEY, false, {
			deserialize: storedBool,
		});
	const [arenaHistoryLimit, setArenaHistoryLimit] = useLocalStorage<number>(
		ARENA_HISTORY_LIMIT_KEY,
		DEFAULT_ARENA_HISTORY_LIMIT,
		{ serialize: String, deserialize: parseArenaHistoryLimit },
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
			for (const key of ARENA_STORAGE_KEYS) localStorage.removeItem(key);
		}
	};

	const setPersistConversation = (v: boolean) => {
		setPersistConversationRaw(v);
		if (!v) {
			// The same content the chat branch drops, for the two-model mode: the
			// transcript and the prompts driving it. The model picks are settings,
			// not content, so they stay.
			localStorage.removeItem("conversationMessages");
			localStorage.removeItem("conversationSystemPromptA");
			localStorage.removeItem("conversationSystemPromptB");
			localStorage.removeItem("conversationActivePersonaIdA");
			localStorage.removeItem("conversationActivePersonaIdB");
		}
	};

	const setArenaHistoryEnabled = (v: boolean) => {
		setArenaHistoryEnabledRaw(v);
		if (!v) {
			localStorage.removeItem(ARENA_HISTORY_KEY);
		}
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
