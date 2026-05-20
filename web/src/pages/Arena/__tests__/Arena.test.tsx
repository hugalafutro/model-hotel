import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

// Mock useArena so we can control phase/rounds without the full hook chain
vi.mock("../useArena", () => ({
	useArena: vi.fn(),
}));

// Stub child components that need full context
vi.mock("../../../components/PageHeader", () => ({
	PageHeader: () => null,
}));
vi.mock("../../../components/SubModeToggle", () => ({
	SubModeToggle: () => null,
}));
vi.mock("../../../components/PromptPicker", () => ({
	PromptPicker: () => null,
}));
vi.mock("../../../components/ModelPicker", () => ({
	ModelPicker: () => null,
}));
vi.mock("../../../components/PersonaPicker", () => ({
	PersonaPicker: () => null,
}));
vi.mock("../../../components/CollapsibleToggle", () => ({
	CollapsibleToggle: () => null,
}));
vi.mock("../../../components/ActionIconButton", () => ({
	ActionIconButton: () => null,
}));
vi.mock("../../../components/ArenaHistoryModal", () => ({
	ArenaHistoryModal: () => null,
}));
vi.mock("../../../components/ConfirmDialog", () => ({
	ConfirmDialog: () => null,
}));
vi.mock("../MatchupCard", () => ({
	MatchupCard: () => null,
}));
vi.mock("../ResponseCard", () => ({
	ResponseCard: () => null,
}));
vi.mock("../ParamEditorModal", () => ({
	ParamEditorModal: () => null,
}));
vi.mock("../WinnerSummaryModal", () => ({
	WinnerSummaryModal: () => null,
}));
vi.mock("../SwapPicker", () => ({
	SwapPicker: () => null,
}));
vi.mock("../shared", () => ({
	BracketPreviewPill: () => null,
}));
vi.mock("../../../data/presets", () => ({
	ARENA_PROMPTS: [],
	CHAT_PERSONAS: [],
}));
vi.mock("../../../utils/model", () => ({
	parseCapabilities: () => ({ supportsVision: false, supportsAudio: false }),
}));

import { Arena } from "../../Arena";
import { useArena } from "../useArena";

const baseArena = {
	phase: "setup" as const,
	currentRound: 0,
	rounds: [] as Array<{
		matchups: Array<{
			slotA: unknown;
			slotB: unknown;
			responseA: unknown;
			responseB: unknown;
			vote: string | null;
		}>;
	}>,
	isRunning: false,
	buttonLabel: null as string | null,
	canRun: false,
	disabledReason: "",
	arenaMode: "competition" as const,
	handleRunArena: vi.fn(),
	handleStopAll: vi.fn(),
	handleReset: vi.fn(),
	handleFullReset: vi.fn(),
	winnerModal: null,
	arenaCollapsed: false,
	setArenaCollapsed: vi.fn(),
	showHistoryModal: false,
	setShowHistoryModal: vi.fn(),
	pendingFullReset: false,
	setPendingFullReset: vi.fn(),
	compareModels: [] as string[],
	bracketModels: [] as string[],
	prompt: "",
	activePromptId: null as string | null,
	savedPrompt: "",
	comparePersonaId: null as string | null,
	comparePersonaPrompt: "",
	roundLabel: () => "",
	showResponseGrid: false,
	arenaIcon: null,
	disabledModels: new Set<string>(),
	runningModels: new Set<string>(),
	modelParams: {} as Record<string, unknown>,
	paramEditorModel: null as string | null,
	enabledModels: [] as unknown[],
	setCompareModels: vi.fn(),
	setBracketModels: vi.fn(),
	setComparePersonaId: vi.fn(),
	setComparePersonaPrompt: vi.fn(),
	setModelParams: vi.fn(),
	setParamEditorModel: vi.fn(),
	setPrompt: vi.fn(),
	setActivePromptId: vi.fn(),
	setSavedPrompt: vi.fn(),
	setArenaMode: vi.fn(),
	onPersonaChange: vi.fn(),
	onModelParamChange: vi.fn(),
};

describe("Arena - voting message guard", () => {
	it('shows "Vote on all matchups" when phase is voting and votes are missing', () => {
		vi.mocked(useArena).mockReturnValue({
			...baseArena,
			phase: "voting",
			currentRound: 0,
			rounds: [
				{
					matchups: [
						{
							slotA: null,
							slotB: null,
							responseA: null,
							responseB: null,
							vote: null,
						},
					],
				},
			],
		} as unknown as ReturnType<typeof useArena>);

		render(<Arena />);
		expect(
			screen.getByText("Vote on all matchups to continue to the next round"),
		).toBeInTheDocument();
	});

	it('hides "Vote on all matchups" when phase is voting but all matchups have votes', () => {
		vi.mocked(useArena).mockReturnValue({
			...baseArena,
			phase: "voting",
			currentRound: 0,
			rounds: [
				{
					matchups: [
						{
							slotA: {
								modelId: "model-a",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							slotB: {
								modelId: "model-b",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							responseA: null,
							responseB: null,
							vote: "A" as const,
						},
					],
				},
			],
		} as unknown as ReturnType<typeof useArena>);

		render(<Arena />);
		expect(
			screen.queryByText("Vote on all matchups to continue to the next round"),
		).not.toBeInTheDocument();
	});
});

describe("Arena - Run/Stop button layout", () => {
	it("renders button with invisible class when buttonLabel is null", () => {
		vi.mocked(useArena).mockReturnValue({
			...baseArena,
			phase: "voting",
			buttonLabel: null,
		} as unknown as ReturnType<typeof useArena>);

		const { container } = render(<Arena />);
		// Button should exist in DOM but be invisible
		const btn = container.querySelector("button.invisible");
		expect(btn).toBeInTheDocument();
		expect(btn).toHaveClass("pointer-events-none");
	});

	it("renders visible button when buttonLabel is set", () => {
		vi.mocked(useArena).mockReturnValue({
			...baseArena,
			phase: "setup",
			buttonLabel: "Run Arena",
			canRun: true,
		} as unknown as ReturnType<typeof useArena>);

		const { container } = render(<Arena />);
		const btn = container.querySelector("button.ui-btn");
		expect(btn).toBeInTheDocument();
		expect(btn).not.toHaveClass("invisible");
		expect(btn).not.toHaveClass("pointer-events-none");
		expect(screen.getByText("Run Arena")).toBeInTheDocument();
	});
});
