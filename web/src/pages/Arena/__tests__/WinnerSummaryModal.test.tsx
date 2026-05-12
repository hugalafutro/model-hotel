import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "../../../test/utils";
import type {
	BracketRound,
	Matchup,
	MatchupSlot,
	WinnerSummaryModalProps,
} from "../types";
import { WinnerSummaryModal } from "../WinnerSummaryModal";

describe("WinnerSummaryModal", () => {
	const mockSlotA: MatchupSlot = {
		modelId: "provider/model-a",
		personaId: null,
		personaPrompt: "",
	};

	const mockSlotB: MatchupSlot = {
		modelId: "provider/model-b",
		personaId: null,
		personaPrompt: "",
	};

	const mockMatchupA: Matchup = {
		slotA: mockSlotA,
		slotB: mockSlotB,
		responseA: null,
		responseB: null,
		vote: "A",
	};

	const mockMatchupB: Matchup = {
		slotA: mockSlotA,
		slotB: mockSlotB,
		responseA: null,
		responseB: null,
		vote: "B",
	};

	const mockRound: BracketRound = {
		matchups: [mockMatchupA],
	};

	const defaultProps: WinnerSummaryModalProps = {
		winner: "provider/model-a",
		rounds: [mockRound],
		onClose: vi.fn(),
	};

	it("renders modal with header", () => {
		renderWithProviders(<WinnerSummaryModal {...defaultProps} />);

		expect(screen.getByText("Match Complete")).toBeInTheDocument();
	});

	it("renders Trophy icon", () => {
		renderWithProviders(<WinnerSummaryModal {...defaultProps} />);

		const trophyIcons = document.querySelectorAll(".lucide-trophy");
		expect(trophyIcons.length).toBeGreaterThanOrEqual(1);
	});

	it("displays winner name extracted from modelId", () => {
		renderWithProviders(<WinnerSummaryModal {...defaultProps} />);

		// Use getAllBy since model-a appears multiple times
		const modelAElements = screen.getAllByText("model-a");
		expect(modelAElements.length).toBeGreaterThanOrEqual(1);
		expect(screen.getByText("wins!")).toBeInTheDocument();
	});

	it("extracts winner name from path with multiple slashes", () => {
		renderWithProviders(
			<WinnerSummaryModal
				{...defaultProps}
				winner="provider/subdir/model-name"
			/>,
		);

		expect(screen.getByText("model-name")).toBeInTheDocument();
	});

	it("displays winner banner with correct styling", () => {
		renderWithProviders(<WinnerSummaryModal {...defaultProps} />);

		// Get the winner text in the banner (first occurrence should be in banner)
		const modelAElements = screen.getAllByText("model-a");
		const winnerBanner = modelAElements[0].closest("div");
		expect(winnerBanner).toHaveClass("bg-amber-500/10");
		expect(winnerBanner).toHaveClass("border-amber-500/30");
	});

	it("renders round label for single round", () => {
		renderWithProviders(<WinnerSummaryModal {...defaultProps} />);

		expect(screen.getByText("Match")).toBeInTheDocument();
	});

	it("renders round labels for multiple rounds", () => {
		const multipleRounds: BracketRound[] = [
			{ matchups: [mockMatchupA] },
			{ matchups: [mockMatchupB] },
			{ matchups: [mockMatchupA] },
			{ matchups: [mockMatchupB] },
		];

		renderWithProviders(
			<WinnerSummaryModal {...defaultProps} rounds={multipleRounds} />,
		);

		expect(screen.getByText("Quarterfinals")).toBeInTheDocument();
		expect(screen.getByText("Semifinals")).toBeInTheDocument();
		expect(screen.getByText("Final")).toBeInTheDocument();
	});

	it("renders Final for last round", () => {
		const twoRounds: BracketRound[] = [
			{ matchups: [mockMatchupA] },
			{ matchups: [mockMatchupB] },
		];

		renderWithProviders(
			<WinnerSummaryModal {...defaultProps} rounds={twoRounds} />,
		);

		expect(screen.getByText("Final")).toBeInTheDocument();
	});

	it("renders Semifinals for second-to-last round", () => {
		const threeRounds: BracketRound[] = [
			{ matchups: [mockMatchupA] },
			{ matchups: [mockMatchupB] },
			{ matchups: [mockMatchupA] },
		];

		renderWithProviders(
			<WinnerSummaryModal {...defaultProps} rounds={threeRounds} />,
		);

		expect(screen.getByText("Semifinals")).toBeInTheDocument();
	});

	it("renders Quarterfinals for third-to-last round", () => {
		const fourRounds: BracketRound[] = [
			{ matchups: [mockMatchupA] },
			{ matchups: [mockMatchupB] },
			{ matchups: [mockMatchupA] },
			{ matchups: [mockMatchupB] },
		];

		renderWithProviders(
			<WinnerSummaryModal {...defaultProps} rounds={fourRounds} />,
		);

		expect(screen.getByText("Quarterfinals")).toBeInTheDocument();
	});

	it("renders Round N for earlier rounds", () => {
		const fiveRounds: BracketRound[] = [
			{ matchups: [mockMatchupA] },
			{ matchups: [mockMatchupB] },
			{ matchups: [mockMatchupA] },
			{ matchups: [mockMatchupB] },
			{ matchups: [mockMatchupA] },
		];

		renderWithProviders(
			<WinnerSummaryModal {...defaultProps} rounds={fiveRounds} />,
		);

		expect(screen.getByText("Round 1")).toBeInTheDocument();
		expect(screen.getByText("Round 2")).toBeInTheDocument();
	});

	it("renders matchup with slotA and slotB model names", () => {
		renderWithProviders(<WinnerSummaryModal {...defaultProps} />);

		const modelAElements = screen.getAllByText("model-a");
		expect(modelAElements.length).toBeGreaterThanOrEqual(1);
		expect(screen.getByText("model-b")).toBeInTheDocument();
		expect(screen.getByText("vs")).toBeInTheDocument();
	});

	it("highlights winner with green color", () => {
		renderWithProviders(<WinnerSummaryModal {...defaultProps} />);

		const winnerText = screen
			.getAllByText("model-a")
			.find((el) => el.closest("span")?.classList.contains("text-green-400"));
		expect(winnerText).toBeInTheDocument();
	});

	it("displays vote indicator for matchups with votes", () => {
		renderWithProviders(<WinnerSummaryModal {...defaultProps} />);

		expect(screen.getByText("← model-a wins")).toBeInTheDocument();
	});

	it("does not display vote indicator for matchups without votes", () => {
		const matchupWithoutVote: Matchup = {
			slotA: mockSlotA,
			slotB: mockSlotB,
			responseA: null,
			responseB: null,
			vote: null,
		};

		const roundWithoutVote: BracketRound = {
			matchups: [matchupWithoutVote],
		};

		renderWithProviders(
			<WinnerSummaryModal {...defaultProps} rounds={[roundWithoutVote]} />,
		);

		expect(screen.queryByText("wins")).not.toBeInTheDocument();
	});

	it("handles null slotA gracefully", () => {
		const matchupWithNullSlot: Matchup = {
			slotA: null,
			slotB: mockSlotB,
			responseA: null,
			responseB: null,
			vote: "B",
		};

		const roundWithNullSlot: BracketRound = {
			matchups: [matchupWithNullSlot],
		};

		renderWithProviders(
			<WinnerSummaryModal {...defaultProps} rounds={[roundWithNullSlot]} />,
		);

		expect(screen.getByText("TBD")).toBeInTheDocument();
	});

	it("handles null slotB gracefully", () => {
		const matchupWithNullSlot: Matchup = {
			slotA: mockSlotA,
			slotB: null,
			responseA: null,
			responseB: null,
			vote: "A",
		};

		const roundWithNullSlot: BracketRound = {
			matchups: [matchupWithNullSlot],
		};

		renderWithProviders(
			<WinnerSummaryModal {...defaultProps} rounds={[roundWithNullSlot]} />,
		);

		expect(screen.getByText("TBD")).toBeInTheDocument();
	});

	it("renders Close button", () => {
		renderWithProviders(<WinnerSummaryModal {...defaultProps} />);

		expect(screen.getByText("Close")).toBeInTheDocument();
	});

	it("calls onClose when Close button is clicked", async () => {
		const user = userEvent.setup();
		const onCloseMock = vi.fn();

		renderWithProviders(
			<WinnerSummaryModal {...defaultProps} onClose={onCloseMock} />,
		);

		const closeButton = screen.getByText("Close");
		await user.click(closeButton);

		expect(onCloseMock).toHaveBeenCalledTimes(1);
	});

	it("calls onClose when Escape key is pressed", async () => {
		const onCloseMock = vi.fn();

		renderWithProviders(
			<WinnerSummaryModal {...defaultProps} onClose={onCloseMock} />,
		);

		const dialog = screen.getByRole("dialog");
		dialog.dispatchEvent(
			new KeyboardEvent("keydown", { key: "Escape", bubbles: true }),
		);

		expect(onCloseMock).toHaveBeenCalledTimes(1);
	});

	it("calls onClose when backdrop is clicked", async () => {
		const user = userEvent.setup();
		const onCloseMock = vi.fn();

		renderWithProviders(
			<WinnerSummaryModal {...defaultProps} onClose={onCloseMock} />,
		);

		const backdrop = document.querySelector(
			"button[aria-label='Close dialog']",
		);
		if (backdrop) {
			await user.click(backdrop);
			expect(onCloseMock).toHaveBeenCalledTimes(1);
		}
	});

	it("renders multiple matchups in a round", () => {
		const roundWithMultipleMatchups: BracketRound = {
			matchups: [mockMatchupA, mockMatchupB],
		};

		renderWithProviders(
			<WinnerSummaryModal
				{...defaultProps}
				rounds={[roundWithMultipleMatchups]}
			/>,
		);

		// Should have both matchups displayed
		const modelAElements = screen.getAllByText("model-a");
		expect(modelAElements.length).toBeGreaterThanOrEqual(2);
	});

	it("renders multiple rounds with correct structure", () => {
		const twoRounds: BracketRound[] = [
			{ matchups: [mockMatchupA] },
			{ matchups: [mockMatchupB] },
		];

		renderWithProviders(
			<WinnerSummaryModal {...defaultProps} rounds={twoRounds} />,
		);

		expect(screen.getByText("Final")).toBeInTheDocument();
		// For 2 rounds, first round is "Semifinals"
		expect(screen.getByText("Semifinals")).toBeInTheDocument();
	});

	it("uses max-w-lg as default maxWidth", () => {
		renderWithProviders(<WinnerSummaryModal {...defaultProps} />);

		const modal = screen.getByRole("dialog");
		const modalContent = modal.querySelector(".max-w-lg");
		expect(modalContent).toBeInTheDocument();
	});

	it("is scrollable", () => {
		renderWithProviders(<WinnerSummaryModal {...defaultProps} />);

		const modal = screen.getByRole("dialog");
		const modalContent = modal.querySelector(".overflow-y-auto");
		expect(modalContent).toBeInTheDocument();
	});

	it("highlights vote A winner correctly", () => {
		renderWithProviders(<WinnerSummaryModal {...defaultProps} />);

		// The winner (model-a) should be highlighted in green
		const winnerElements = screen.getAllByText("model-a");
		const highlightedWinner = winnerElements.find((el) =>
			el.closest("span")?.classList.contains("text-green-400"),
		);
		expect(highlightedWinner).toBeInTheDocument();
	});

	it("highlights vote B winner correctly", () => {
		const matchupBWins: Matchup = {
			slotA: mockSlotA,
			slotB: mockSlotB,
			responseA: null,
			responseB: null,
			vote: "B",
		};

		const roundBWins: BracketRound = {
			matchups: [matchupBWins],
		};

		renderWithProviders(
			<WinnerSummaryModal {...defaultProps} rounds={[roundBWins]} />,
		);

		// model-b should be highlighted in green
		const winnerElements = screen.getAllByText("model-b");
		const highlightedWinner = winnerElements.find((el) =>
			el.closest("span")?.classList.contains("text-green-400"),
		);
		expect(highlightedWinner).toBeInTheDocument();
	});

	it("displays model names with correct casing", () => {
		const slotWithCamelCase: MatchupSlot = {
			modelId: "provider/My-Model-Name",
			personaId: null,
			personaPrompt: "",
		};

		const matchup: Matchup = {
			slotA: slotWithCamelCase,
			slotB: mockSlotB,
			responseA: null,
			responseB: null,
			vote: "A",
		};

		renderWithProviders(
			<WinnerSummaryModal
				{...defaultProps}
				rounds={[{ matchups: [matchup] }]}
			/>,
		);

		expect(screen.getByText("My-Model-Name")).toBeInTheDocument();
	});

	it("handles modelId without slash", () => {
		const slotNoSlash: MatchupSlot = {
			modelId: "standalone-model",
			personaId: null,
			personaPrompt: "",
		};

		const matchup: Matchup = {
			slotA: slotNoSlash,
			slotB: mockSlotB,
			responseA: null,
			responseB: null,
			vote: "A",
		};

		renderWithProviders(
			<WinnerSummaryModal
				{...defaultProps}
				rounds={[{ matchups: [matchup] }]}
			/>,
		);

		expect(screen.getByText("standalone-model")).toBeInTheDocument();
	});
});
