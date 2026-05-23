import { useEffect, useRef } from "react";
import type { GenerationParams } from "../../api/types";
import type { ArenaSubMode } from "../../context/SidebarModeContext";
import { useStorage } from "../../context/StorageContext";
import { useToast } from "../../context/ToastContext";
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

export function useArenaPersistence(state: ArenaPersistenceState) {
	const { persistArena } = useStorage();
	const { toast } = useToast();
	const quotaWarnedRef = useRef(false);

	useEffect(() => {
		if (!persistArena) return;
		try {
			localStorage.setItem(
				"arenaState",
				JSON.stringify({
					arenaMode: state.arenaMode,
					compareModels: state.compareModels,
					bracketModels: state.bracketModels,
					rounds: state.rounds,
					currentRound: state.currentRound,
					phase: state.phase,
					arenaCollapsed: state.arenaCollapsed,
					savedPrompt: state.savedPrompt,
					modelParams: state.modelParams,
				}),
			);
		} catch {
			/* quota exceeded */
			if (!quotaWarnedRef.current) {
				quotaWarnedRef.current = true;
				toast("Storage full - arena state not saved", "warning");
			}
		}
	}, [
		state.arenaMode,
		state.compareModels,
		state.bracketModels,
		state.rounds,
		state.currentRound,
		state.phase,
		state.arenaCollapsed,
		state.savedPrompt,
		state.modelParams,
		persistArena,
		toast,
	]);
}
