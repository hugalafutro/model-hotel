import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { AllProviders } from "../../../test/utils";
import { MatchupCard } from "../MatchupCard";
import type { MatchupSlot } from "../types";

const mockSlot: MatchupSlot = {
	modelId: "test-provider/gemma-3b",
	personaId: "assistant",
	personaPrompt: "You are a helpful assistant.",
	params: { temperature: 0.7, max_tokens: 1024 },
};

const defaultProps = {
	slot: mockSlot,
	slotKey: "A" as const,
	roundIdx: 0,
	matchupIdx: 0,
	vote: null as "A" | "B" | null,
	response: null,
	isRunning: false,
	phase: "setup" as const,
	onPersonaChange: vi.fn(),
	onVote: vi.fn(),
};

describe("MatchupCard", () => {
	beforeEach(() => {
		vi.clearAllMocks();
	});

	describe("rendering matchup info", () => {
		it("renders model name from modelId", () => {
			render(<MatchupCard {...defaultProps} />, { wrapper: AllProviders });

			expect(screen.getByText("gemma-3b")).toBeInTheDocument();
		});

		it("renders Bot icon", () => {
			render(<MatchupCard {...defaultProps} />, { wrapper: AllProviders });

			// Bot icon is rendered as SVG with class lucide-bot
			const botIcon = document.querySelector(".lucide-bot");
			expect(botIcon).toBeInTheDocument();
		});

		it("renders slot params tooltip when params exist", () => {
			render(<MatchupCard {...defaultProps} />, { wrapper: AllProviders });

			// Settings icon for params tooltip has title attribute
			const settingsIcon = document.querySelector(".lucide-settings");
			expect(settingsIcon).toBeInTheDocument();
		});

		it("does not render params tooltip when no params", () => {
			const slotWithoutParams: MatchupSlot = {
				modelId: "test-provider/model",
				personaId: null,
				personaPrompt: "",
			};

			render(<MatchupCard {...defaultProps} slot={slotWithoutParams} />, {
				wrapper: AllProviders,
			});

			expect(screen.queryByTestId("lucide-settings")).not.toBeInTheDocument();
		});

		it("renders TBD when slot is null", () => {
			render(<MatchupCard {...defaultProps} slot={null} />, {
				wrapper: AllProviders,
			});

			expect(screen.getByText("TBD")).toBeInTheDocument();
		});
	});

	describe("slots", () => {
		it("displays slotKey A correctly", () => {
			const { container } = render(<MatchupCard {...defaultProps} />, {
				wrapper: AllProviders,
			});

			// The card should render with proper structure
			expect(container.firstChild).toHaveClass("rounded-lg");
		});

		it("applies winner styling when isWinner", () => {
			render(
				<MatchupCard
					{...defaultProps}
					vote="A"
					phase="voting"
					response={{
						model: "test",
						rawContent: "",
						content: "",
						thinkingContent: "",
						startTimeMs: 0,
						done: true,
						error: null,
						metrics: null,
					}}
				/>,
				{ wrapper: AllProviders },
			);

			const card = screen.getByText("gemma-3b").closest(".rounded-lg");
			expect(card).toHaveClass("bg-green-500/10");
			expect(card).toHaveClass("border-green-500/40");
		});

		it("applies loser styling when isLoser", () => {
			render(
				<MatchupCard
					{...defaultProps}
					vote="B"
					phase="voting"
					response={{
						model: "test",
						rawContent: "",
						content: "",
						thinkingContent: "",
						startTimeMs: 0,
						done: true,
						error: null,
						metrics: null,
					}}
				/>,
				{ wrapper: AllProviders },
			);

			const card = screen.getByText("gemma-3b").closest(".rounded-lg");
			expect(card).toHaveClass("bg-red-500/5");
			expect(card).toHaveClass("opacity-60");
		});
	});

	describe("model names", () => {
		it("extracts model name from path", () => {
			const slotWithDeepPath: MatchupSlot = {
				modelId: "provider/subdir/model-name",
				personaId: null,
				personaPrompt: "",
			};

			render(<MatchupCard {...defaultProps} slot={slotWithDeepPath} />, {
				wrapper: AllProviders,
			});

			expect(screen.getByText("model-name")).toBeInTheDocument();
		});

		it("handles modelId without slash", () => {
			const slotNoSlash: MatchupSlot = {
				modelId: "standalone-model",
				personaId: null,
				personaPrompt: "",
			};

			render(<MatchupCard {...defaultProps} slot={slotNoSlash} />, {
				wrapper: AllProviders,
			});

			expect(screen.getByText("standalone-model")).toBeInTheDocument();
		});
	});

	describe("swap button", () => {
		it("does not render swap button in setup phase", () => {
			render(<MatchupCard {...defaultProps} phase="setup" />, {
				wrapper: AllProviders,
			});

			// No vote button in setup phase
			expect(
				screen.queryByRole("button", { name: /vote/i }),
			).not.toBeInTheDocument();
		});

		it("renders vote button in voting phase when response is done", () => {
			render(
				<MatchupCard
					{...defaultProps}
					phase="voting"
					response={{
						model: "test",
						rawContent: "",
						content: "",
						thinkingContent: "",
						startTimeMs: 0,
						done: true,
						error: null,
						metrics: null,
					}}
				/>,
				{ wrapper: AllProviders },
			);

			// Vote thumb button should be present
			const voteButton = screen.getByRole("button");
			expect(voteButton).toBeInTheDocument();
		});

		it("calls onVote when vote button is clicked", async () => {
			const onVoteMock = vi.fn();

			render(
				<MatchupCard
					{...defaultProps}
					phase="voting"
					onVote={onVoteMock}
					response={{
						model: "test",
						rawContent: "",
						content: "",
						thinkingContent: "",
						startTimeMs: 0,
						done: true,
						error: null,
						metrics: null,
					}}
				/>,
				{ wrapper: AllProviders },
			);

			const voteButton = screen.getByRole("button");
			fireEvent.click(voteButton);

			expect(onVoteMock).toHaveBeenCalledWith(0, 0, "A");
		});

		it("disables vote button after voting", () => {
			render(
				<MatchupCard
					{...defaultProps}
					vote="A"
					phase="voting"
					response={{
						model: "test",
						rawContent: "",
						content: "",
						thinkingContent: "",
						startTimeMs: 0,
						done: true,
						error: null,
						metrics: null,
					}}
				/>,
				{ wrapper: AllProviders },
			);

			const voteButton = screen.getByRole("button");
			expect(voteButton).toHaveAttribute("disabled");
		});
	});

	describe("retry button", () => {
		it("shows error icon when response has error", () => {
			const { container } = render(
				<MatchupCard
					{...defaultProps}
					response={{
						model: "test",
						rawContent: "",
						content: "",
						thinkingContent: "",
						startTimeMs: 0,
						done: false,
						error: "Connection timeout",
						metrics: null,
					}}
				/>,
				{ wrapper: AllProviders },
			);

			// AlertCircle icon for error - check by text content containing error message or by SVG
			// The error icon renders as an SVG element
			const errorIcons = container.querySelectorAll("svg");
			// Find the one that's the error indicator (red color)
			const errorIcon = Array.from(errorIcons).find(
				(svg) =>
					svg.querySelector(".text-red-400") ||
					svg.classList.contains("text-red-400"),
			);
			expect(errorIcon).toBeInTheDocument();
		});

		it("does not show error icon when no error", () => {
			render(
				<MatchupCard
					{...defaultProps}
					response={{
						model: "test",
						rawContent: "",
						content: "",
						thinkingContent: "",
						startTimeMs: 0,
						done: true,
						error: null,
						metrics: null,
					}}
				/>,
				{ wrapper: AllProviders },
			);

			expect(
				screen.queryByTestId("lucide-alert-circle"),
			).not.toBeInTheDocument();
		});
	});

	describe("loading state", () => {
		it("shows loading indicator when isRunning", () => {
			const { container } = render(
				<MatchupCard
					{...defaultProps}
					isRunning
					response={{
						model: "test",
						rawContent: "",
						content: "",
						thinkingContent: "",
						startTimeMs: 0,
						done: false,
						error: null,
						metrics: null,
					}}
				/>,
				{ wrapper: AllProviders },
			);

			// Loading pulse indicator - span with animate-pulse class
			const loadingIndicator = container.querySelector("span.animate-pulse");
			expect(loadingIndicator).toBeInTheDocument();
		});

		it("does not show loading indicator when not running", () => {
			const { container } = render(
				<MatchupCard {...defaultProps} isRunning={false} />,
				{
					wrapper: AllProviders,
				},
			);

			expect(
				container.querySelector("span.animate-pulse"),
			).not.toBeInTheDocument();
		});
	});

	describe("winner state", () => {
		it("shows trophy icon when phase is finished and isWinner", () => {
			render(
				<MatchupCard
					{...defaultProps}
					vote="A"
					phase="finished"
					response={{
						model: "test",
						rawContent: "",
						content: "",
						thinkingContent: "",
						startTimeMs: 0,
						done: true,
						error: null,
						metrics: null,
					}}
				/>,
				{ wrapper: AllProviders },
			);

			// Trophy icon for winner
			const trophyIcon = document.querySelector(".lucide-trophy");
			expect(trophyIcon).toBeInTheDocument();
		});

		it("does not show trophy icon when not winner", () => {
			render(
				<MatchupCard
					{...defaultProps}
					vote="B"
					phase="finished"
					response={{
						model: "test",
						rawContent: "",
						content: "",
						thinkingContent: "",
						startTimeMs: 0,
						done: true,
						error: null,
						metrics: null,
					}}
				/>,
				{ wrapper: AllProviders },
			);

			expect(document.querySelector(".lucide-trophy")).not.toBeInTheDocument();
		});
	});

	describe("PresetBar rendering", () => {
		it("renders PresetBar in setup phase for round 0", () => {
			render(<MatchupCard {...defaultProps} phase="setup" roundIdx={0} />, {
				wrapper: AllProviders,
			});

			// PresetBar should render with custom button and persona items
			const customButton = screen.getByRole("button", { name: /✏️/i });
			expect(customButton).toBeInTheDocument();
		});

		it("does not render PresetBar in running phase", () => {
			render(<MatchupCard {...defaultProps} phase="running" roundIdx={0} />, {
				wrapper: AllProviders,
			});

			expect(
				screen.queryByRole("button", { name: /✏️/i }),
			).not.toBeInTheDocument();
		});

		it("does not render PresetBar for rounds > 0", () => {
			render(<MatchupCard {...defaultProps} phase="setup" roundIdx={1} />, {
				wrapper: AllProviders,
			});

			expect(
				screen.queryByRole("button", { name: /✏️/i }),
			).not.toBeInTheDocument();
		});

		it("calls onPersonaChange when preset is selected", async () => {
			const onPersonaChangeMock = vi.fn();

			render(
				<MatchupCard
					{...defaultProps}
					phase="setup"
					roundIdx={0}
					onPersonaChange={onPersonaChangeMock}
				/>,
				{ wrapper: AllProviders },
			);

			// Click on a persona preset button (e.g., "🤖Unit 734")
			const unitButton = screen.getByRole("button", {
				name: "🤖Unit 734",
			});
			fireEvent.click(unitButton);

			expect(onPersonaChangeMock).toHaveBeenCalledWith(
				0,
				0,
				"A",
				"unit-734",
				expect.any(String),
			);
		});

		it("shows confirm dialog when switching from custom to preset", async () => {
			const slotWithCustom: MatchupSlot = {
				modelId: "test-provider/model",
				personaId: null,
				personaPrompt: "Custom prompt",
			};

			render(
				<MatchupCard
					{...defaultProps}
					slot={slotWithCustom}
					phase="setup"
					roundIdx={0}
				/>,
				{ wrapper: AllProviders },
			);

			// Click on a preset when already has custom prompt
			const unitButton = screen.getByRole("button", {
				name: "🤖Unit 734",
			});
			fireEvent.click(unitButton);

			// Confirm dialog should appear
			expect(screen.getByText("Overwrite Persona")).toBeInTheDocument();
		});
	});

	describe("ConfirmDialog", () => {
		it("renders confirm dialog when pendingPersona is set", () => {
			// This is internal state, so we test by triggering the condition
			const slotWithCustom: MatchupSlot = {
				modelId: "test-provider/model",
				personaId: null,
				personaPrompt: "Custom",
			};

			render(
				<MatchupCard
					{...defaultProps}
					slot={slotWithCustom}
					phase="setup"
					roundIdx={0}
				/>,
				{ wrapper: AllProviders },
			);

			// Click preset to trigger pending state
			const unitButton = screen.getByRole("button", {
				name: "🤖Unit 734",
			});
			fireEvent.click(unitButton);

			// Dialog should render
			expect(screen.getByText("Overwrite Persona")).toBeInTheDocument();
			expect(screen.getByText("Discard")).toBeInTheDocument();
			expect(screen.getByText("Cancel")).toBeInTheDocument();
		});

		it("calls onConfirm when confirm is clicked", () => {
			const onPersonaChangeMock = vi.fn();
			const slotWithCustom: MatchupSlot = {
				modelId: "test-provider/model",
				personaId: null,
				personaPrompt: "Custom",
			};

			render(
				<MatchupCard
					{...defaultProps}
					slot={slotWithCustom}
					phase="setup"
					roundIdx={0}
					onPersonaChange={onPersonaChangeMock}
				/>,
				{ wrapper: AllProviders },
			);

			// Trigger dialog
			const unitButton = screen.getByRole("button", {
				name: "🤖Unit 734",
			});
			fireEvent.click(unitButton);

			// Click discard
			const discardButton = screen.getByText("Discard");
			fireEvent.click(discardButton);

			expect(onPersonaChangeMock).toHaveBeenCalled();
		});

		it("calls onCancel when cancel is clicked", () => {
			const slotWithCustom: MatchupSlot = {
				modelId: "test-provider/model",
				personaId: null,
				personaPrompt: "Custom",
			};

			render(
				<MatchupCard
					{...defaultProps}
					slot={slotWithCustom}
					phase="setup"
					roundIdx={0}
				/>,
				{ wrapper: AllProviders },
			);

			// Trigger dialog
			const unitButton = screen.getByRole("button", {
				name: "🤖Unit 734",
			});
			fireEvent.click(unitButton);

			// Click cancel
			const cancelButton = screen.getByText("Cancel");
			fireEvent.click(cancelButton);

			// Dialog should close (no longer in document)
			expect(screen.queryByText("Overwrite Persona")).not.toBeInTheDocument();
		});
	});
});
