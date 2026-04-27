import { createContext, useContext, useState, useCallback, type ReactNode } from "react";

export type ChatSubMode = "chat" | "conversation";
export type ArenaSubMode = "competition" | "compare";

interface SidebarModeContextType {
    chatSubMode: ChatSubMode;
    setChatSubMode: (v: ChatSubMode) => void;
    arenaSubMode: ArenaSubMode;
    setArenaSubMode: (v: ArenaSubMode) => void;
}

const SidebarModeContext = createContext<SidebarModeContextType>({
    chatSubMode: "chat",
    setChatSubMode: () => {},
    arenaSubMode: "competition",
    setArenaSubMode: () => {},
});

// eslint-disable-next-line react-refresh/only-export-components
export function useSidebarMode() {
    return useContext(SidebarModeContext);
}

const CHAT_SUB_MODE_KEY = "sidebarChatSubMode";
const ARENA_SUB_MODE_KEY = "sidebarArenaSubMode";

export function SidebarModeProvider({ children }: { children: ReactNode }) {
    const [chatSubMode, setChatSubModeState] = useState<ChatSubMode>(() => {
        try {
            const v = localStorage.getItem(CHAT_SUB_MODE_KEY);
            if (v === "conversation") return "conversation";
            return "chat";
        } catch {
            return "chat";
        }
    });

    const [arenaSubMode, setArenaSubModeState] = useState<ArenaSubMode>(() => {
        try {
            const v = localStorage.getItem(ARENA_SUB_MODE_KEY);
            if (v === "compare") return "compare";
            return "competition";
        } catch {
            return "competition";
        }
    });

    const setChatSubMode = useCallback((v: ChatSubMode) => {
        setChatSubModeState(v);
        try {
            localStorage.setItem(CHAT_SUB_MODE_KEY, v);
        } catch {
            /* ignore */
        }
    }, []);

    const setArenaSubMode = useCallback((v: ArenaSubMode) => {
        setArenaSubModeState(v);
        try {
            localStorage.setItem(ARENA_SUB_MODE_KEY, v);
        } catch {
            /* ignore */
        }
    }, []);

    return (
        <SidebarModeContext.Provider
            value={{
                chatSubMode,
                setChatSubMode,
                arenaSubMode,
                setArenaSubMode,
            }}
        >
            {children}
        </SidebarModeContext.Provider>
    );
}
