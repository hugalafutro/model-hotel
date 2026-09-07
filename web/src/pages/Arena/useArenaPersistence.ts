import type { GenerationParams } from "../../api/types";
import type { ArenaSubMode } from "../../context/SidebarModeContext";
import { useStorage } from "../../context/StorageContext";
import { usePersistedJSON } from "../../hooks/usePersistedJSON";
import type { BracketPhase, BracketRound } from "./types";

export interface ArenaPersistenceState {
	arenaMode: ArenaSubMode;
	compareModels: string[];
	bracketModels: string[];
	rounds: BracketRound[];
	currentRound: number;
	phase: BracketPhase;
	arenaCollapsed: boolean;
	savedPrompt: string;
	modelParams: Record<string, GenerationParams>;
}

/**
 * Mirrors the whole arena board into `arenaState`. The caller passes a
 * snapshot that only changes when one of its fields does, so a render that
 * changed nothing does not rewrite the blob.
 */
export function useArenaPersistence(state: ArenaPersistenceState) {
	const { persistArena } = useStorage();
	usePersistedJSON(
		"arenaState",
		state,
		persistArena,
		"hooks.useArenaPersistence.storageFullArena",
	);
}
