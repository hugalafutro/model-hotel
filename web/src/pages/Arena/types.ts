import type { GenerationParams, Model } from "../../api/types";

export interface ArenaResponse {
	model: string;
	rawContent: string;
	content: string;
	thinkingContent: string;
	startTimeMs: number;
	done: boolean;
	error: string | null;
	metrics: {
		tokensPerSecond: number | null;
		durationMs: number;
		promptTokens: number;
		completionTokens: number;
	} | null;
}

export interface MatchupSlot {
	modelId: string;
	personaId: string | null;
	personaPrompt: string;
	params?: GenerationParams;
}

export interface Matchup {
	slotA: MatchupSlot | null;
	slotB: MatchupSlot | null;
	responseA: ArenaResponse | null;
	responseB: ArenaResponse | null;
	vote: "A" | "B" | null;
	/** Render slot B on the left. Drawn at random when the matchup is formed, so
	 * the blind cards cannot be matched to the setup order. */
	flipped?: boolean;
}

export interface BracketRound {
	matchups: Matchup[];
}

export type BracketPhase =
	| "setup"
	| "running"
	| "voting"
	| "next_round_ready"
	| "finished";

export interface WinnerModal {
	winner: string;
	rounds: BracketRound[];
}

export interface MatchupCardProps {
	slot: MatchupSlot | null;
	slotKey: "A" | "B";
	roundIdx: number;
	matchupIdx: number;
	vote: "A" | "B" | null;
	response: ArenaResponse | null;
	isRunning: boolean;
	phase: BracketPhase;
	/** Hide the model's identity (competition mode, before the matchup is voted). */
	blind?: boolean;
	onPersonaChange: (
		roundIdx: number,
		matchupIdx: number,
		slot: "A" | "B",
		personaId: string | null,
		personaPrompt: string,
	) => void;
	onVote: (roundIdx: number, matchupIdx: number, vote: "A" | "B") => void;
}

export interface ResponseCardProps {
	response: ArenaResponse;
	vote: "A" | "B" | null;
	slotKey: "A" | "B";
	roundIdx: number;
	matchupIdx: number;
	onVote: (roundIdx: number, matchupIdx: number, vote: "A" | "B") => void;
	onRetry: (roundIdx: number, matchupIdx: number, slotKey: "A" | "B") => void;
	onSwapModel: (
		roundIdx: number,
		matchupIdx: number,
		slotKey: "A" | "B",
		failedModelId: string,
	) => void;
	onCancelSlot: (
		roundIdx: number,
		matchupIdx: number,
		slotKey: "A" | "B",
		modelId: string,
	) => void;
	showVote: boolean;
	/** Hide the model's identity (competition mode, before the matchup is voted). */
	blind?: boolean;
	enabledModels: Model[];
	params?: GenerationParams;
}

export interface SwapPickerProps {
	enabledModels: Array<{
		provider_name: string;
		model_id: string;
		display_name?: string;
		enabled?: boolean;
	}>;
	disabledModels: Set<string>;
	alreadyUsed: string[];
	onSelect: (modelId: string) => void;
}

export interface WinnerSummaryModalProps {
	winner: string;
	rounds: BracketRound[];
	onClose: () => void;
}
