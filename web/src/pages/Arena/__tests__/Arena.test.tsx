import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

// Controllable mocks for components we need to interact with
interface MockActionIconButtonProps {
	title: string;
	onClick?: () => void;
}
interface MockConfirmDialogProps {
	onConfirm?: () => void;
	onCancel?: () => void;
	confirmLabel?: string;
}
const mockActionIconButton =
	vi.fn<(props: MockActionIconButtonProps) => React.ReactNode>();
const mockConfirmDialog =
	vi.fn<(props: MockConfirmDialogProps) => React.ReactNode>();

beforeEach(() => {
	mockActionIconButton.mockImplementation(
		({ title, onClick }: MockActionIconButtonProps) => (
			<button
				type="button"
				aria-label={title}
				onClick={onClick}
				data-testid={`action-${title}`}
			>
				{title}
			</button>
		),
	);
	mockConfirmDialog.mockImplementation(
		({ onConfirm, onCancel, confirmLabel }: MockConfirmDialogProps) => (
			<div data-testid="confirm-dialog">
				<button type="button" onClick={onConfirm} data-testid="confirm-btn">
					{confirmLabel ?? "Confirm"}
				</button>
				<button type="button" onClick={onCancel} data-testid="cancel-btn">
					Cancel
				</button>
			</div>
		),
	);
});

// Mock useArena so we can control phase/rounds without the full hook chain
vi.mock("../useArena", () => ({
	useArena: vi.fn(),
}));

// Stub child components that need full context
vi.mock("../../../components/PageHeader", () => ({
	PageHeader: ({
		title,
		description,
	}: {
		title: string;
		description?: React.ReactNode;
	}) => (
		<div data-testid="page-header">
			<span data-testid="page-title">{title}</span>
			<span data-testid="page-description">{description}</span>
		</div>
	),
}));
vi.mock("../../../components/SubModeToggle", () => ({
	SubModeToggle: ({
		onChange,
		value,
		disabled,
	}: {
		onChange: (v: string) => void;
		value: string;
		disabled?: boolean;
	}) => (
		<div
			data-testid="submode-toggle"
			data-value={value}
			data-disabled={disabled}
		>
			<button
				type="button"
				onClick={() => onChange("compare")}
				data-testid="submode-btn"
			>
				Toggle
			</button>
		</div>
	),
}));
vi.mock("../../../components/PromptPicker", () => ({
	PromptPicker: ({
		prompt,
		disabled,
	}: {
		prompt: string;
		disabled?: boolean;
	}) => (
		<div
			data-testid="prompt-picker"
			data-prompt={prompt}
			data-disabled={disabled}
		/>
	),
}));
vi.mock("../../../components/ModelPicker", () => ({
	ModelPicker: ({
		id,
		selected,
	}: {
		id?: string;
		selected: string | string[];
	}) => (
		<div
			data-testid={`model-picker-${id}`}
			data-selected={JSON.stringify(selected)}
		/>
	),
}));
vi.mock("../../../components/PersonaPicker", () => ({
	PersonaPicker: ({ activePersonaId }: { activePersonaId: string | null }) => (
		<div data-testid="persona-picker" data-persona={activePersonaId} />
	),
}));
vi.mock("../../../components/CollapsibleToggle", () => ({
	CollapsibleToggle: ({
		collapsed,
		onToggle,
	}: {
		collapsed: boolean;
		onToggle: () => void;
	}) => (
		<button
			type="button"
			onClick={onToggle}
			data-testid="collapsible-toggle"
			data-collapsed={collapsed}
		>
			Toggle
		</button>
	),
}));
vi.mock("../../../components/ActionIconButton", () => ({
	ActionIconButton: (props: MockActionIconButtonProps) =>
		mockActionIconButton(props),
}));
vi.mock("../../../components/ArenaHistoryModal", () => ({
	ArenaHistoryModal: ({ onClose }: { onClose: () => void }) => (
		<div data-testid="arena-history-modal">
			<button type="button" onClick={onClose}>
				Close
			</button>
		</div>
	),
}));
vi.mock("../../../components/ConfirmDialog", () => ({
	ConfirmDialog: (props: MockConfirmDialogProps) => mockConfirmDialog(props),
}));
vi.mock("../MatchupCard", () => ({
	MatchupCard: () => null,
}));
vi.mock("../ResponseCard", () => ({
	ResponseCard: () => null,
}));
vi.mock("../ParamEditorModal", () => ({
	ParamEditorModal: ({
		modelId,
		onClose,
	}: {
		modelId: string;
		onClose: () => void;
	}) => (
		<div data-testid="param-editor-modal" data-model={modelId}>
			<button type="button" onClick={onClose}>
				Close
			</button>
		</div>
	),
}));
vi.mock("../WinnerSummaryModal", () => ({
	WinnerSummaryModal: ({
		winner,
		onClose,
	}: {
		winner: string;
		onClose: () => void;
	}) => (
		<div data-testid="winner-summary-modal" data-winner={winner}>
			<button type="button" onClick={onClose}>
				Close
			</button>
		</div>
	),
}));
vi.mock("../SwapPicker", () => ({
	SwapPicker: () => null,
}));
vi.mock("../shared", () => ({
	BracketPreviewPill: ({
		modelId,
		displayName,
		isTbd,
	}: {
		modelId: string;
		displayName?: string;
		isTbd?: boolean;
	}) => (
		<span
			data-testid="bracket-pill"
			data-model={modelId}
			data-display={displayName}
			data-tbd={isTbd ?? false}
		>
			{displayName ?? modelId}
		</span>
	),
}));
vi.mock("../../../data/presets", () => ({
	ARENA_PROMPTS: [],
	CHAT_PERSONAS: [],
}));
vi.mock("../../../utils/model", () => ({
	parseCapabilities: () => ({
		supportsVision: false,
		supportsAudio: false,
		reasoning: false,
	}),
	proxyModelID: (provider: string, modelId: string) => `${provider}/${modelId}`,
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
	refs: { abortMapRef: { current: new Map() } },
	handleRandomBracketModel: vi.fn(),
	handleRandomCompareModel: vi.fn(),
	handleRandomComparePersona: vi.fn(),
	handleRetrySlot: vi.fn(),
	handleSwapModel: vi.fn(),
	handleCancelSlot: vi.fn(),
	handleSwapCompleteAndUpdate: vi.fn(),
	handlePersonaChange: vi.fn(),
	handleVote: vi.fn(),
	competitionPrompt: "",
	comparePrompt: "",
	competitionActivePromptId: null as string | null,
	compareActivePromptId: null as string | null,
	setCompetitionActivePromptId: vi.fn(),
	setCompareActivePromptId: vi.fn(),
	setCompetitionPrompt: vi.fn(),
	setComparePrompt: vi.fn(),
	setRounds: vi.fn(),
	setCurrentRound: vi.fn(),
	setPhase: vi.fn(),
	setRunningModels: vi.fn(),
	setWinnerModal: vi.fn(),
	setDisabledModels: vi.fn(),
	toast: vi.fn(),
	previewPairs: null as Array<{ a: string; b: string }> | null,
};

function mockArena(overrides: Record<string, unknown> = {}) {
	vi.mocked(useArena).mockReturnValue({
		...baseArena,
		...overrides,
	} as unknown as ReturnType<typeof useArena>);
}

describe("Arena - voting message guard", () => {
	it('shows "Vote on all matchups" when phase is voting and votes are missing', () => {
		mockArena({
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
		});

		render(<Arena />);
		expect(
			screen.getByText("Vote on all matchups to continue to the next round"),
		).toBeInTheDocument();
	});

	it('hides "Vote on all matchups" when phase is voting but all matchups have votes', () => {
		mockArena({
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
		});

		render(<Arena />);
		expect(
			screen.queryByText("Vote on all matchups to continue to the next round"),
		).not.toBeInTheDocument();
	});
});

describe("Arena - Run/Stop button layout", () => {
	it("renders button with invisible class when buttonLabel is null", () => {
		mockArena({ phase: "voting", buttonLabel: null });

		const { container } = render(<Arena />);
		const btn = container.querySelector("button.invisible");
		expect(btn).toBeInTheDocument();
		expect(btn).toHaveClass("pointer-events-none");
	});

	it("renders visible button when buttonLabel is set", () => {
		mockArena({ phase: "setup", buttonLabel: "Run Arena", canRun: true });

		const { container } = render(<Arena />);
		const btn = container.querySelector("button.ui-btn");
		expect(btn).toBeInTheDocument();
		expect(btn).not.toHaveClass("invisible");
		expect(btn).not.toHaveClass("pointer-events-none");
		expect(screen.getByText("Run Arena")).toBeInTheDocument();
	});
});

describe("Arena - PageHeader", () => {
	it('shows "Arena" title for competition mode', () => {
		mockArena({ arenaMode: "competition" });
		render(<Arena />);
		expect(screen.getByTestId("page-title")).toHaveTextContent("Arena");
	});

	it('shows "Compare" title for compare mode', () => {
		mockArena({ arenaMode: "compare" });
		render(<Arena />);
		expect(screen.getByTestId("page-title")).toHaveTextContent("Compare");
	});

	it("shows competition description text", () => {
		mockArena({ arenaMode: "competition" });
		render(<Arena />);
		expect(screen.getByTestId("page-description")).toHaveTextContent(
			"Bracket tournament - models compete head-to-head",
		);
	});

	it("shows compare description text", () => {
		mockArena({ arenaMode: "compare" });
		render(<Arena />);
		expect(screen.getByTestId("page-description")).toHaveTextContent(
			"Side-by-side - compare model outputs on the same prompt",
		);
	});
});

describe("Arena - Controls section", () => {
	it('renders "Controls" label', () => {
		mockArena();
		render(<Arena />);
		expect(screen.getByText("Controls")).toBeInTheDocument();
	});

	it("renders match history button", () => {
		mockArena();
		render(<Arena />);
		expect(
			screen.getByRole("button", { name: "Match history" }),
		).toBeInTheDocument();
	});

	it("hides reset buttons when phase is setup with no models/prompt/persona", () => {
		mockArena({
			phase: "setup",
			bracketModels: [],
			compareModels: [],
			activePromptId: null,
			prompt: "",
			comparePersonaId: null,
			comparePersonaPrompt: "",
		});

		render(<Arena />);
		expect(
			screen.queryByTestId("action-Clear results (keep models & prompt)"),
		).not.toBeInTheDocument();
		expect(
			screen.queryByTestId("action-Reset all (clear models & prompt)"),
		).not.toBeInTheDocument();
	});

	it("shows reset buttons when phase is setup but has bracket models", () => {
		mockArena({ phase: "setup", bracketModels: ["model1"] });
		render(<Arena />);
		expect(
			screen.getByTestId("action-Reset all (clear models & prompt)"),
		).toBeInTheDocument();
	});

	it("shows reset buttons when phase is setup but has compare models", () => {
		mockArena({
			phase: "setup",
			arenaMode: "compare",
			compareModels: ["model1"],
		});
		render(<Arena />);
		expect(
			screen.getByTestId("action-Reset all (clear models & prompt)"),
		).toBeInTheDocument();
	});

	it("shows reset buttons when phase is setup but has activePromptId", () => {
		mockArena({ phase: "setup", activePromptId: "prompt-1" });
		render(<Arena />);
		expect(
			screen.getByTestId("action-Reset all (clear models & prompt)"),
		).toBeInTheDocument();
	});

	it("shows reset buttons when phase is setup but has prompt text", () => {
		mockArena({ phase: "setup", prompt: "test prompt" });
		render(<Arena />);
		expect(
			screen.getByTestId("action-Reset all (clear models & prompt)"),
		).toBeInTheDocument();
	});

	it("shows reset buttons when phase is setup but has compare persona", () => {
		mockArena({
			phase: "setup",
			arenaMode: "compare",
			comparePersonaId: "persona-1",
		});
		render(<Arena />);
		expect(
			screen.getByTestId("action-Reset all (clear models & prompt)"),
		).toBeInTheDocument();
	});

	it("shows reset buttons when phase is setup but has compare persona prompt", () => {
		mockArena({
			phase: "setup",
			arenaMode: "compare",
			comparePersonaPrompt: "test prompt",
		});
		render(<Arena />);
		expect(
			screen.getByTestId("action-Reset all (clear models & prompt)"),
		).toBeInTheDocument();
	});

	it("shows both light and full reset buttons when phase is not setup", () => {
		mockArena({ phase: "running" });
		render(<Arena />);
		expect(
			screen.getByTestId("action-Clear results (keep models & prompt)"),
		).toBeInTheDocument();
		expect(
			screen.getByTestId("action-Reset all (clear models & prompt)"),
		).toBeInTheDocument();
	});

	it("hides light reset (Eraser) button in setup phase even with models", () => {
		mockArena({ phase: "setup", bracketModels: ["model1"] });
		render(<Arena />);
		expect(
			screen.queryByTestId("action-Clear results (keep models & prompt)"),
		).not.toBeInTheDocument();
	});

	it("shows light reset (Eraser) button when phase is not setup", () => {
		mockArena({ phase: "running" });
		render(<Arena />);
		expect(
			screen.getByTestId("action-Clear results (keep models & prompt)"),
		).toBeInTheDocument();
	});

	it("light reset onClick aborts controllers and resets arena state", async () => {
		const abortSpy = vi.fn();
		const setRoundsSpy = vi.fn();
		const setCurrentRoundSpy = vi.fn();
		const setPhaseSpy = vi.fn();
		const setRunningModelsSpy = vi.fn();
		const setWinnerModalSpy = vi.fn();
		const setDisabledModelsSpy = vi.fn();
		const toastSpy = vi.fn();

		const abortMap = new Map();
		abortMap.set("key1", { abort: abortSpy });
		vi.spyOn(abortMap, "clear");

		mockArena({
			phase: "running",
			refs: { abortMapRef: { current: abortMap } },
			setRounds: setRoundsSpy,
			setCurrentRound: setCurrentRoundSpy,
			setPhase: setPhaseSpy,
			setRunningModels: setRunningModelsSpy,
			setWinnerModal: setWinnerModalSpy,
			setDisabledModels: setDisabledModelsSpy,
			toast: toastSpy,
		});

		const user = userEvent.setup();
		render(<Arena />);
		const eraserBtn = screen.getByTestId(
			"action-Clear results (keep models & prompt)",
		);
		await user.click(eraserBtn);

		expect(abortSpy).toHaveBeenCalled();
		expect(abortMap.clear).toHaveBeenCalled();
		expect(setRoundsSpy).toHaveBeenCalledWith([]);
		expect(setCurrentRoundSpy).toHaveBeenCalledWith(0);
		expect(setPhaseSpy).toHaveBeenCalledWith("setup");
		expect(setRunningModelsSpy).toHaveBeenCalledWith(new Set());
		expect(setWinnerModalSpy).toHaveBeenCalledWith(null);
		expect(setDisabledModelsSpy).toHaveBeenCalledWith(new Set());
		expect(toastSpy).toHaveBeenCalledWith("Arena cleared", "info");
	});

	it("full reset onClick sets pendingFullReset to true", async () => {
		const setPendingFullResetSpy = vi.fn();
		mockArena({
			phase: "setup",
			bracketModels: ["model1"],
			setPendingFullReset: setPendingFullResetSpy,
		});

		const user = userEvent.setup();
		render(<Arena />);
		await user.click(
			screen.getByTestId("action-Reset all (clear models & prompt)"),
		);
		expect(setPendingFullResetSpy).toHaveBeenCalledWith(true);
	});

	it("clicking match history button sets showHistoryModal to true", async () => {
		const setShowHistoryModalSpy = vi.fn();
		mockArena({ setShowHistoryModal: setShowHistoryModalSpy });

		const user = userEvent.setup();
		render(<Arena />);
		await user.click(screen.getByRole("button", { name: "Match history" }));
		expect(setShowHistoryModalSpy).toHaveBeenCalledWith(true);
	});

	it("renders CollapsibleToggle and toggles collapsed state", async () => {
		const setArenaCollapsed = vi.fn();
		mockArena({ arenaCollapsed: true, setArenaCollapsed });

		const { container } = render(<Arena />);
		expect(container.querySelector(".grid-rows-\\[0fr\\]")).toBeInTheDocument();

		const user = userEvent.setup();
		await user.click(screen.getByTestId("collapsible-toggle"));
		expect(setArenaCollapsed).toHaveBeenCalled();
	});

	it("SubModeToggle onChange calls setArenaMode when phase is setup", async () => {
		const setArenaModeSpy = vi.fn();
		mockArena({ phase: "setup", setArenaMode: setArenaModeSpy });

		const user = userEvent.setup();
		render(<Arena />);
		await user.click(screen.getByTestId("submode-btn"));
		expect(setArenaModeSpy).toHaveBeenCalledWith("compare");
	});

	it("SubModeToggle is disabled when phase is not setup", () => {
		mockArena({ phase: "running" });
		render(<Arena />);
		expect(screen.getByTestId("submode-toggle")).toHaveAttribute(
			"data-disabled",
			"true",
		);
	});
});

describe("Arena - Setup content", () => {
	it("renders bracket models picker in competition setup", () => {
		mockArena({ phase: "setup", arenaMode: "competition" });
		render(<Arena />);
		expect(screen.getByText("Models (0/8)")).toBeInTheDocument();
		expect(
			screen.getByText("Pick 2, 4, or 8 for a bracket"),
		).toBeInTheDocument();
		expect(
			screen.getByTestId("model-picker-bracket-models-picker"),
		).toBeInTheDocument();
	});

	it("renders compare models picker in compare setup", () => {
		mockArena({ phase: "setup", arenaMode: "compare" });
		render(<Arena />);
		expect(screen.getByText("Models (0/6)")).toBeInTheDocument();
		expect(
			screen.getByTestId("model-picker-compare-models-picker"),
		).toBeInTheDocument();
	});

	it("renders persona picker in compare setup", () => {
		mockArena({ phase: "setup", arenaMode: "compare" });
		render(<Arena />);
		expect(screen.getByTestId("persona-picker")).toBeInTheDocument();
	});

	it("PromptPicker gets prompt in setup phase", () => {
		mockArena({ phase: "setup", prompt: "test prompt" });
		render(<Arena />);
		expect(screen.getByTestId("prompt-picker")).toHaveAttribute(
			"data-prompt",
			"test prompt",
		);
	});

	it("PromptPicker gets savedPrompt in running phase", () => {
		mockArena({ phase: "running", savedPrompt: "saved prompt" });
		render(<Arena />);
		expect(screen.getByTestId("prompt-picker")).toHaveAttribute(
			"data-prompt",
			"saved prompt",
		);
	});

	it("PromptPicker is disabled during running/voting phase", () => {
		mockArena({ phase: "running" });
		render(<Arena />);
		expect(screen.getByTestId("prompt-picker")).toHaveAttribute(
			"data-disabled",
			"true",
		);
	});
});

describe("Arena - Preview", () => {
	it('renders preview pairs with "First Round" label', () => {
		mockArena({ phase: "setup", previewPairs: [{ a: "P/m1", b: "P/m2" }] });
		render(<Arena />);
		expect(screen.getByText("First Round")).toBeInTheDocument();
	});

	it("renders compare model preview pills in compare mode", () => {
		mockArena({
			phase: "setup",
			arenaMode: "compare",
			compareModels: ["P/m1", "P/m2"],
		});
		render(<Arena />);
		const pills = screen.getAllByTestId("bracket-pill");
		expect(pills).toHaveLength(2);
	});
});

describe("Arena - Rounds rendering", () => {
	it("renders rounds with round labels when rounds exist", () => {
		mockArena({
			phase: "setup",
			rounds: [
				{
					matchups: [
						{
							slotA: {
								modelId: "m1",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							slotB: {
								modelId: "m2",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							responseA: null,
							responseB: null,
							vote: null,
						},
					],
				},
			],
			roundLabel: (idx: number) => `Round ${idx + 1}`,
		});

		render(<Arena />);
		expect(screen.getByText("Round 1")).toBeInTheDocument();
	});

	it("hides past rounds in non-setup phase", () => {
		mockArena({
			phase: "running",
			currentRound: 1,
			roundLabel: (idx: number) => `Round ${idx + 1}`,
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
		});

		render(<Arena />);
		expect(screen.queryByText("Round 1")).not.toBeInTheDocument();
		expect(screen.getByText("Round 2")).toBeInTheDocument();
	});

	it("shows only last round in finished phase", () => {
		mockArena({
			phase: "finished",
			roundLabel: (idx: number) => `Round ${idx + 1}`,
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
		});

		render(<Arena />);
		expect(screen.queryByText("Round 1")).not.toBeInTheDocument();
		expect(screen.getByText("Round 2")).toBeInTheDocument();
	});

	it("applies opacity-30 for far future rounds", () => {
		mockArena({
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
		});

		const { container } = render(<Arena />);
		// Round 2 (idx 2 > currentRound 0 + 1) should have opacity-30
		expect(
			container.querySelectorAll('[class*="opacity-30"]').length,
		).toBeGreaterThan(0);
	});

	it("applies opacity-50 for near future rounds", () => {
		mockArena({
			phase: "running",
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
		});

		const { container } = render(<Arena />);
		// Round 1 (idx > currentRound but idx <= currentRound + 1) should have opacity-50
		expect(
			container.querySelectorAll('[class*="opacity-50"]').length,
		).toBeGreaterThan(0);
	});

	it("applies opacity-100 for current/past rounds", () => {
		mockArena({
			phase: "running",
			currentRound: 1,
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
		});

		const { container } = render(<Arena />);
		expect(
			container.querySelectorAll('[class*="opacity-100"]').length,
		).toBeGreaterThan(0);
	});
});

describe("Arena - Run/Stop button", () => {
	it('shows "Stop All" when isRunning is true', () => {
		mockArena({ isRunning: true, buttonLabel: "Stop All" });
		render(<Arena />);
		expect(screen.getByText("Stop All")).toBeInTheDocument();
	});

	it('shows "Run Arena" when phase is setup and not running', () => {
		mockArena({
			phase: "setup",
			isRunning: false,
			buttonLabel: "Run Arena",
			canRun: true,
		});
		render(<Arena />);
		expect(screen.getByText("Run Arena")).toBeInTheDocument();
	});

	it("calls handleStopAll when clicked while running", () => {
		const handleStopAll = vi.fn();
		mockArena({
			phase: "running",
			isRunning: true,
			buttonLabel: "Stop All",
			handleStopAll,
		});

		const { container } = render(<Arena />);
		const btn = container.querySelector("button.ui-btn") as HTMLElement;
		fireEvent.click(btn);
		expect(handleStopAll).toHaveBeenCalled();
	});

	it("calls handleRunArena when clicked while not running", async () => {
		const handleRunArena = vi.fn();
		mockArena({
			phase: "setup",
			isRunning: false,
			buttonLabel: "Run Arena",
			canRun: true,
			handleRunArena,
		});

		const user = userEvent.setup();
		render(<Arena />);
		const btn = screen.getByRole("button", { name: /run arena/i });
		await user.click(btn);
		expect(handleRunArena).toHaveBeenCalled();
	});

	it("disables button when buttonLabel is null", () => {
		mockArena({ buttonLabel: null });
		const { container } = render(<Arena />);
		const btn = container.querySelector("button.ui-btn");
		expect(btn).toBeDisabled();
	});

	it("disables button in setup when canRun is false", () => {
		mockArena({ phase: "setup", buttonLabel: "Run Arena", canRun: false });
		const { container } = render(<Arena />);
		const btn = container.querySelector("button.ui-btn");
		expect(btn).toBeDisabled();
	});

	it("has danger class when running", () => {
		mockArena({ isRunning: true, buttonLabel: "Stop All" });
		const { container } = render(<Arena />);
		const btn = container.querySelector("button.ui-btn");
		expect(btn).toHaveClass("ui-btn-danger");
	});

	it("has primary class when not running", () => {
		mockArena({ isRunning: false, buttonLabel: "Run Arena", canRun: true });
		const { container } = render(<Arena />);
		const btn = container.querySelector("button.ui-btn");
		expect(btn).toHaveClass("ui-btn-primary");
	});
});

describe("Arena - Button message IIFE", () => {
	it("shows disabledReason in setup when canRun is false", () => {
		mockArena({
			phase: "setup",
			canRun: false,
			disabledReason: "Pick 2+ models",
		});
		render(<Arena />);
		expect(screen.getByText("Pick 2+ models")).toBeInTheDocument();
	});

	it("shows 'Models are generating' in running phase with isRunning", () => {
		mockArena({ phase: "running", isRunning: true });
		render(<Arena />);
		expect(screen.getByText(/models are generating/i)).toBeInTheDocument();
	});

	it("shows pulse dot when models are generating", () => {
		mockArena({ phase: "running", isRunning: true });
		const { container } = render(<Arena />);
		expect(container.querySelector(".animate-pulse")).toBeInTheDocument();
	});

	it("shows disabledReason in next_round_ready when canRun is false", () => {
		mockArena({
			phase: "next_round_ready",
			canRun: false,
			disabledReason: "Some models disabled",
		});
		render(<Arena />);
		expect(screen.getByText("Some models disabled")).toBeInTheDocument();
	});

	it("shows 'Start the next round when ready' as fallback in next_round_ready", () => {
		mockArena({ phase: "next_round_ready", canRun: false, disabledReason: "" });
		render(<Arena />);
		expect(
			screen.getByText("Start the next round when ready"),
		).toBeInTheDocument();
	});
});

describe("Arena - Mode description", () => {
	it("shows competition mode description", () => {
		mockArena({ arenaMode: "competition" });
		render(<Arena />);
		expect(
			screen.getByText(/Models compete in a single-elimination bracket/),
		).toBeInTheDocument();
	});

	it("shows compare mode description", () => {
		mockArena({ arenaMode: "compare" });
		render(<Arena />);
		expect(
			screen.getByText(/Pick models and run the same prompt/),
		).toBeInTheDocument();
	});
});

describe("Arena - ConfirmDialog full reset", () => {
	it("renders ConfirmDialog when pendingFullReset is true", () => {
		mockArena({ pendingFullReset: true });
		render(<Arena />);
		expect(screen.getByTestId("confirm-dialog")).toBeInTheDocument();
	});

	it("onConfirm clears all state and localStorage", async () => {
		const abortMap = new Map();
		abortMap.set("key1", { abort: vi.fn() });
		const clearSpy = vi.spyOn(abortMap, "clear");
		const setPendingFullResetSpy = vi.fn();
		const toastSpy = vi.fn();

		const arenaReturn = {
			...baseArena,
			pendingFullReset: true,
			refs: { abortMapRef: { current: abortMap } },
			setCompareModels: vi.fn(),
			setBracketModels: vi.fn(),
			setCompetitionPrompt: vi.fn(),
			setComparePrompt: vi.fn(),
			setSavedPrompt: vi.fn(),
			setCompetitionActivePromptId: vi.fn(),
			setCompareActivePromptId: vi.fn(),
			setComparePersonaId: vi.fn(),
			setComparePersonaPrompt: vi.fn(),
			setRounds: vi.fn(),
			setCurrentRound: vi.fn(),
			setPhase: vi.fn(),
			setRunningModels: vi.fn(),
			setWinnerModal: vi.fn(),
			setDisabledModels: vi.fn(),
			setModelParams: vi.fn(),
			setPendingFullReset: setPendingFullResetSpy,
			toast: toastSpy,
		} as unknown as ReturnType<typeof useArena>;

		vi.mocked(useArena).mockReturnValue(arenaReturn);

		// Set localStorage keys so we can verify they are removed
		const lsKeys = [
			"arenaCompetitionPrompt",
			"arenaComparePrompt",
			"arenaCompetitionActivePromptId",
			"arenaCompareActivePromptId",
			"arenaComparePersonaId",
			"arenaComparePersonaPrompt",
		];
		for (const key of lsKeys) {
			localStorage.setItem(key, "test");
		}

		try {
			const user = userEvent.setup();
			render(<Arena />);
			await user.click(screen.getByTestId("confirm-btn"));

			expect(clearSpy).toHaveBeenCalled();
			expect(arenaReturn.setCompareModels).toHaveBeenCalledWith([]);
			expect(arenaReturn.setBracketModels).toHaveBeenCalledWith([]);
			expect(arenaReturn.setCompetitionPrompt).toHaveBeenCalledWith("");
			expect(arenaReturn.setComparePrompt).toHaveBeenCalledWith("");
			expect(arenaReturn.setSavedPrompt).toHaveBeenCalledWith("");
			expect(arenaReturn.setCompetitionActivePromptId).toHaveBeenCalledWith(
				null,
			);
			expect(arenaReturn.setCompareActivePromptId).toHaveBeenCalledWith(null);
			expect(arenaReturn.setComparePersonaId).toHaveBeenCalledWith(null);
			expect(arenaReturn.setComparePersonaPrompt).toHaveBeenCalledWith("");
			expect(arenaReturn.setRounds).toHaveBeenCalledWith([]);
			expect(arenaReturn.setCurrentRound).toHaveBeenCalledWith(0);
			expect(arenaReturn.setPhase).toHaveBeenCalledWith("setup");
			expect(arenaReturn.setRunningModels).toHaveBeenCalledWith(new Set());
			expect(arenaReturn.setWinnerModal).toHaveBeenCalledWith(null);
			expect(arenaReturn.setDisabledModels).toHaveBeenCalledWith(new Set());
			expect(arenaReturn.setModelParams).toHaveBeenCalledWith({});
			expect(setPendingFullResetSpy).toHaveBeenCalledWith(false);
			expect(toastSpy).toHaveBeenCalledWith("Reset", "info");

			// Verify localStorage cleanup
			for (const key of lsKeys) {
				expect(localStorage.getItem(key)).toBeNull();
			}
		} finally {
			// Clean up localStorage even if assertions fail
			for (const key of lsKeys) {
				localStorage.removeItem(key);
			}
		}
	});

	it("onCancel sets pendingFullReset to false", async () => {
		const setPendingFullResetSpy = vi.fn();
		mockArena({
			pendingFullReset: true,
			setPendingFullReset: setPendingFullResetSpy,
		});

		const user = userEvent.setup();
		render(<Arena />);
		await user.click(screen.getByTestId("cancel-btn"));
		expect(setPendingFullResetSpy).toHaveBeenCalledWith(false);
	});
});

describe("Arena - Modals", () => {
	it("renders ArenaHistoryModal when showHistoryModal is true", () => {
		mockArena({ showHistoryModal: true });
		render(<Arena />);
		expect(screen.getByTestId("arena-history-modal")).toBeInTheDocument();
	});

	it("renders WinnerSummaryModal when winnerModal is not null", () => {
		mockArena({ winnerModal: { winner: "model1", rounds: [] } });
		render(<Arena />);
		expect(screen.getByTestId("winner-summary-modal")).toBeInTheDocument();
	});

	it("renders ParamEditorModal when paramEditorModel is not null", () => {
		mockArena({
			paramEditorModel: "provider/model1",
			enabledModels: [
				{
					provider_name: "provider",
					model_id: "model1",
					display_name: "Model 1",
					name: "model1",
					capabilities: "",
				},
			],
		});
		render(<Arena />);
		expect(screen.getByTestId("param-editor-modal")).toBeInTheDocument();
	});

	it("does not render ParamEditorModal when paramEditorModel is null", () => {
		mockArena({ paramEditorModel: null });
		render(<Arena />);
		expect(screen.queryByTestId("param-editor-modal")).not.toBeInTheDocument();
	});
});

describe("Arena - Auto-scroll", () => {
	it("does not scroll when not isRunning", () => {
		const scrollToSpy = vi
			.spyOn(window, "scrollTo")
			.mockImplementation(() => {});
		mockArena({ isRunning: false });
		render(<Arena />);
		expect(scrollToSpy).not.toHaveBeenCalled();
		scrollToSpy.mockRestore();
	});

	it("scrolls to bottom when isRunning and near bottom", () => {
		const scrollToSpy = vi
			.spyOn(window, "scrollTo")
			.mockImplementation(() => {});
		mockArena({ isRunning: true });
		render(<Arena />);
		expect(scrollToSpy).toHaveBeenCalledWith(
			expect.objectContaining({ behavior: "instant" }),
		);
		scrollToSpy.mockRestore();
	});
});
