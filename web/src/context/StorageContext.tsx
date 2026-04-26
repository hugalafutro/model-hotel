import {
    createContext,
    useContext,
    useState,
    type ReactNode,
} from "react";

interface StorageContextType {
    persistChat: boolean;
    setPersistChat: (v: boolean) => void;
    persistArena: boolean;
    setPersistArena: (v: boolean) => void;
}

const StorageContext = createContext<StorageContextType>({
    persistChat: false,
    setPersistChat: () => {},
    persistArena: false,
    setPersistArena: () => {},
});

// eslint-disable-next-line react-refresh/only-export-components
export function useStorage() {
    return useContext(StorageContext);
}

const CHAT_KEY = "persistChat";
const ARENA_KEY = "persistArena";

export function StorageProvider({ children }: { children: ReactNode }) {
    const [persistChat, setPersistChatState] = useState(() => {
        return localStorage.getItem(CHAT_KEY) === "true";
    });
    const [persistArena, setPersistArenaState] = useState(() => {
        return localStorage.getItem(ARENA_KEY) === "true";
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
        }
    };

    return (
        <StorageContext.Provider
            value={{ persistChat, setPersistChat, persistArena, setPersistArena }}
        >
            {children}
        </StorageContext.Provider>
    );
}