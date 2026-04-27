import { createContext, useContext, useState, type ReactNode } from "react";

interface StorageContextType {
    persistChat: boolean;
    setPersistChat: (v: boolean) => void;
    persistArena: boolean;
    setPersistArena: (v: boolean) => void;
    persistConversation: boolean;
    setPersistConversation: (v: boolean) => void;
}

const StorageContext = createContext<StorageContextType>({
    persistChat: false,
    setPersistChat: () => {},
    persistArena: false,
    setPersistArena: () => {},
    persistConversation: false,
    setPersistConversation: () => {},
});

// eslint-disable-next-line react-refresh/only-export-components
export function useStorage() {
    return useContext(StorageContext);
}

const CHAT_KEY = "persistChat";
const ARENA_KEY = "persistArena";
const CONVERSATION_KEY = "persistConversation";

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
            localStorage.removeItem("arenaPrompt");
            localStorage.removeItem("arenaActivePromptId");
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

    return (
        <StorageContext.Provider
            value={{
                persistChat,
                setPersistChat,
                persistArena,
                setPersistArena,
                persistConversation,
                setPersistConversation,
            }}
        >
            {children}
        </StorageContext.Provider>
    );
}
