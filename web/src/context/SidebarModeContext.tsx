import {
    createContext,
    useContext,
    useState,
    useCallback,
    type ReactNode,
} from "react";

export type ChatSubMode = "chat" | "conversation";
export type ArenaSubMode = "competition" | "compare";
export type LogsSubMode = "request" | "app";

interface SidebarModeContextType {
    chatSubMode: ChatSubMode;
    setChatSubMode: (v: ChatSubMode) => void;
    arenaSubMode: ArenaSubMode;
    setArenaSubMode: (v: ArenaSubMode) => void;
    logsSubMode: LogsSubMode;
    setLogsSubMode: (v: LogsSubMode) => void;
}

const SidebarModeContext = createContext<SidebarModeContextType>({
    chatSubMode: "chat",
    setChatSubMode: () => {},
    arenaSubMode: "competition",
    setArenaSubMode: () => {},
    logsSubMode: "request",
    setLogsSubMode: () => {},
});

// eslint-disable-next-line react-refresh/only-export-components
export function useSidebarMode() {
    return useContext(SidebarModeContext);
}

const CHAT_SUB_MODE_KEY = "sidebarChatSubMode";
const ARENA_SUB_MODE_KEY = "sidebarArenaSubMode";
const LOGS_SUB_MODE_KEY = "sidebarLogsSubMode";

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

    const [logsSubMode, setLogsSubModeState] = useState<LogsSubMode>(() => {
        try {
            const v = localStorage.getItem(LOGS_SUB_MODE_KEY);
            if (v === "app") return "app";
            return "request";
        } catch {
            return "request";
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

    const setLogsSubMode = useCallback((v: LogsSubMode) => {
        setLogsSubModeState(v);
        try {
            localStorage.setItem(LOGS_SUB_MODE_KEY, v);
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
                logsSubMode,
                setLogsSubMode,
            }}
        >
            {children}
        </SidebarModeContext.Provider>
    );
}
