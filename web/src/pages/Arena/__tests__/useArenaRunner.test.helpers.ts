import type { ArenaSubMode } from "../../../context/SidebarModeContext";
import type { useToast } from "../../../context/ToastContext";
import type { BracketRound } from "../types";
import type { useArenaRunner } from "../useArenaRunner";

export const createWrapper = () => {
	return function Wrapper({ children }: { children: React.ReactNode }) {
		return children;
	};
};

export const createMockDeps = (
	overrides?: Partial<Parameters<typeof useArenaRunner>[0]>,
) => {
	const providedRoundsRef = overrides?.roundsRef;
	const roundsRef = providedRoundsRef ?? { current: [] as BracketRound[] };
	const setRoundsMock = vi.fn((fn) => {
		if (typeof fn === "function") {
			const result = fn(roundsRef.current);
			roundsRef.current = result;
		}
	});
	const baseDeps: Parameters<typeof useArenaRunner>[0] = {
		arenaModeRef: { current: "compare" as ArenaSubMode },
		savedPrompt: "Test prompt",
		prompt: "Test prompt",
		setRounds: setRoundsMock,
		setPhase: vi.fn(),
		setRunningModels: vi.fn(),
		rounds: [],
		roundsRef,
		modelParams: {},
		enabledModels: [
			{ provider_name: "P", model_id: "model-a" },
			{ provider_name: "P", model_id: "model-b" },
			{ provider_name: "P", model_id: "new-model" },
		],
		modelsReady: true,
		toast: vi.fn() as ReturnType<typeof useToast>["toast"],
		...overrides,
	};
	return baseDeps;
};
