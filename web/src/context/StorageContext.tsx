import { createContext, useContext, useState, type ReactNode } from "react";

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

const CHAT_KEY = "persistChat";
const ARENA_KEY = "persistArena";
const CONVERSATION_KEY = "persistConversation";
const ARENA_HISTORY_ENABLED_KEY = "arenaHistoryEnabled";
const ARENA_HISTORY_LIMIT_KEY = "arenaHistoryLimit";

export function StorageProvider({ children }: { children: ReactNode }) {
    const [persistChat, setPersistChatState] = useState(() => {
        return localStorage.getItem(CHAT_KEY) === "true";
    });
    const [persistArena, setPersistArenaState] = useState(() => {
        return localStorage.getItem(ARENA_KEY) === "true";
    });
    const [persistConversation, setPersistConversationState] = useState(() => {
        return localStorage.getItem(CONVERSATION_KEY) === "true";
    });
    const [arenaHistoryEnabled, setArenaHistoryEnabledState] = useState(() => {
        return localStorage.getItem(ARENA_HISTORY_ENABLED_KEY) === "true";
    });
    const [arenaHistoryLimit, setArenaHistoryLimitState] = useState(() => {
        try {
            const raw = localStorage.getItem(ARENA_HISTORY_LIMIT_KEY);
            if (raw !== null) {
                const parsed = parseInt(raw, 10);
                if (!isNaN(parsed) && parsed > 0) return parsed;
            }
        } catch {
            /* ignore */
        }
        return 25;
    });

    const setPersistChat = (v: boolean) => {
        setPersistChatState(v);
        localStorage.setItem(CHAT_KEY, String(v));
        if (!v) {
            localStorage.removeItem("chatMessages");
            localStorage.removeItem("chatSystemPrompt");
            localStorage.removeItem("chatActivePersonaId");
        }
    };

    const setPersistArena = (v: boolean) => {
        setPersistArenaState(v);
        localStorage.setItem(ARENA_KEY, String(v));
        if (!v) {
            localStorage.removeItem("arenaCompetitionPrompt");
            localStorage.removeItem("arenaComparePrompt");
            localStorage.removeItem("arenaCompetitionActivePromptId");
            localStorage.removeItem("arenaCompareActivePromptId");
            localStorage.removeItem("arenaState");
        }
    };

    const setPersistConversation = (v: boolean) => {
        setPersistConversationState(v);
        localStorage.setItem(CONVERSATION_KEY, String(v));
        if (!v) {
            localStorage.removeItem("chatConversationMessages");
            localStorage.removeItem("chatConversationState");
        }
    };

    const setArenaHistoryEnabled = (v: boolean) => {
        setArenaHistoryEnabledState(v);
        localStorage.setItem(ARENA_HISTORY_ENABLED_KEY, String(v));
        if (!v) {
            localStorage.removeItem("arenaMatchHistory");
        }
    };

    const setArenaHistoryLimit = (n: number) => {
        setArenaHistoryLimitState(n);
        localStorage.setItem(ARENA_HISTORY_LIMIT_KEY, String(n));
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
