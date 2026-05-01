import { createContext, type ReactNode, useContext } from "react";
import { useLocalStorage } from "../hooks/useLocalStorage";

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

export function SidebarModeProvider({ children }: { children: ReactNode }) {
	const [chatSubMode, setChatSubMode] = useLocalStorage<ChatSubMode>(
		"sidebarChatSubMode",
		"chat",
		{ deserialize: (v) => (v === "conversation" ? "conversation" : "chat") },
	);

	const [arenaSubMode, setArenaSubMode] = useLocalStorage<ArenaSubMode>(
		"sidebarArenaSubMode",
		"competition",
		{ deserialize: (v) => (v === "compare" ? "compare" : "competition") },
	);

	const [logsSubMode, setLogsSubMode] = useLocalStorage<LogsSubMode>(
		"sidebarLogsSubMode",
		"request",
		{ deserialize: (v) => (v === "app" ? "app" : "request") },
	);

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
