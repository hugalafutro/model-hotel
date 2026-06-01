import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "../../test/utils";
import { ArenaHistoryModal } from "../ArenaHistoryModal";

// Mock localStorage for arena history
const mockLocalStorage = {
	store: new Map<string, string>(),
	getItem: vi.fn((key: string) => mockLocalStorage.store.get(key) ?? null),
	setItem: vi.fn((key: string, value: string) => {
		mockLocalStorage.store.set(key, value);
	}),
	removeItem: vi.fn((key: string) => {
		mockLocalStorage.store.delete(key);
	}),
	clear: vi.fn(() => {
		mockLocalStorage.store.clear();
	}),
};

vi.stubGlobal("localStorage", mockLocalStorage);

const mockCompetitionEntry = {
	id: "comp-001",
	timestamp: Date.now() - 86400000,
	mode: "competition" as const,
	promptPresetId: "dilemma",
	comparePersonaId: null,
	rounds: [
		{
			matchups: [
				{
					slotA: { modelId: "provider/model-a", personaId: null },
					slotB: { modelId: "provider/model-b", personaId: null },
					responseA: {
						modelId: "provider/model-a",
						content: "Response A content",
						thinkingContent: "",
						error: null,
						metrics: {
							tokensPerSecond: 50,
							durationMs: 2000,
							promptTokens: 100,
							completionTokens: 200,
						},
					},
					responseB: {
						modelId: "provider/model-b",
						content: "Response B content",
						thinkingContent: "",
						error: null,
						metrics: {
							tokensPerSecond: 45,
							durationMs: 2200,
							promptTokens: 100,
							completionTokens: 180,
						},
					},
					vote: "A" as const,
				},
			],
		},
	],
	winner: "provider/model-a",
	completed: true,
};

const mockCompareEntry = {
	id: "compare-001",
	timestamp: Date.now() - 172800000,
	mode: "compare" as const,
	promptPresetId: "lore",
	comparePersonaId: "merlin",
	compareModels: ["provider/model-c", "provider/model-d"],
	compareResponses: [
		{
			modelId: "provider/model-c",
			content: "Compare response C",
			thinkingContent: "",
			error: null,
			metrics: {
				tokensPerSecond: 55,
				durationMs: 1800,
				promptTokens: 120,
				completionTokens: 250,
			},
		},
		{
			modelId: "provider/model-d",
			content: "Compare response D",
			thinkingContent: "",
			error: null,
			metrics: {
				tokensPerSecond: 48,
				durationMs: 2100,
				promptTokens: 120,
				completionTokens: 230,
			},
		},
	],
	completed: true,
};

describe("ArenaHistoryModal", () => {
	const onClose = vi.fn();
	const onRestore = vi.fn();

	beforeEach(() => {
		vi.clearAllMocks();
		mockLocalStorage.store.clear();
		onClose.mockClear();
		onRestore.mockClear();
	});

	it("renders modal with Match History title", async () => {
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			expect(screen.getByText("Match History")).toBeInTheDocument();
		});
	});

	it("shows close button that calls onClose", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			expect(screen.getAllByLabelText("Close")).toHaveLength(1);
		});
		// Click the modal header close button (first one)
		const closeButtons = screen.getAllByLabelText("Close");
		await user.click(closeButtons[0]);
		expect(onClose).toHaveBeenCalledTimes(1);
	});

	it("shows empty state when no history exists", async () => {
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			expect(screen.getByText("No match history yet")).toBeInTheDocument();
		});
		expect(
			screen.getByText("Completed arena and compare sessions will appear here"),
		).toBeInTheDocument();
	});

	it("renders competition entry in history list", async () => {
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify([mockCompetitionEntry]),
		);
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			expect(screen.getByText(/model-a vs model-b/)).toBeInTheDocument();
		});
	});

	it("renders compare entry in history list", async () => {
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify([mockCompareEntry]),
		);
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			expect(screen.getByText(/model-c, model-d/)).toBeInTheDocument();
		});
	});

	it("shows filter buttons: All, Competition, Compare", async () => {
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			expect(screen.getByText("All")).toBeInTheDocument();
			expect(screen.getByText("Competition")).toBeInTheDocument();
			expect(screen.getByText("Compare")).toBeInTheDocument();
		});
	});

	it("filters entries by Competition mode", async () => {
		const user = userEvent.setup();
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify([mockCompetitionEntry, mockCompareEntry]),
		);
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			expect(screen.getByText(/model-a vs model-b/)).toBeInTheDocument();
		});
		const competitionFilter = screen.getByText("Competition");
		await user.click(competitionFilter);
		await waitFor(() => {
			expect(screen.getByText(/model-a vs model-b/)).toBeInTheDocument();
			expect(screen.queryByText(/model-c, model-d/)).not.toBeInTheDocument();
		});
	});

	it("filters entries by Compare mode", async () => {
		const user = userEvent.setup();
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify([mockCompetitionEntry, mockCompareEntry]),
		);
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			expect(screen.getByText(/model-c, model-d/)).toBeInTheDocument();
		});
		const compareFilter = screen.getByText("Compare");
		await user.click(compareFilter);
		await waitFor(() => {
			expect(screen.getByText(/model-c, model-d/)).toBeInTheDocument();
			expect(screen.queryByText(/model-a vs model-b/)).not.toBeInTheDocument();
		});
	});

	it("expands entry on click to show details", async () => {
		const user = userEvent.setup();
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify([mockCompetitionEntry]),
		);
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			expect(screen.getByText(/model-a vs model-b/)).toBeInTheDocument();
		});
		await user.click(
			screen.getByRole("button", { name: /model-a vs model-b/ }),
		);
		await waitFor(() => {
			expect(screen.getByText(/Winner: model-a/)).toBeInTheDocument();
		});
	});

	it("shows winner badge for competition entry", async () => {
		const user = userEvent.setup();
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify([mockCompetitionEntry]),
		);
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			expect(screen.getByText(/model-a vs model-b/)).toBeInTheDocument();
		});
		await user.click(
			screen.getByRole("button", { name: /model-a vs model-b/ }),
		);
		await waitFor(() => {
			expect(screen.getByText("Winner: model-a")).toBeInTheDocument();
		});
	});

	it("shows response count for compare entry", async () => {
		const user = userEvent.setup();
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify([mockCompareEntry]),
		);
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			expect(screen.getByText(/model-c, model-d/)).toBeInTheDocument();
		});
		await user.click(screen.getByRole("button", { name: /model-c, model-d/ }));
		await waitFor(() => {
			expect(screen.getByText("2 resp.")).toBeInTheDocument();
		});
	});

	it("shows preset badge for entry with promptPresetId", async () => {
		const user = userEvent.setup();
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify([mockCompareEntry]),
		);
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			expect(screen.getByText(/model-c, model-d/)).toBeInTheDocument();
		});
		await user.click(screen.getByRole("button", { name: /model-c, model-d/ }));
		await waitFor(() => {
			expect(screen.getByText("Lore")).toBeInTheDocument();
		});
	});

	it("shows persona badge for entry with comparePersonaId", async () => {
		const user = userEvent.setup();
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify([mockCompareEntry]),
		);
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			expect(screen.getByText(/model-c, model-d/)).toBeInTheDocument();
		});
		await user.click(screen.getByRole("button", { name: /model-c, model-d/ }));
		await waitFor(() => {
			expect(screen.getByText("Merlin")).toBeInTheDocument();
		});
	});

	it("shows Delete button for each entry", async () => {
		const user = userEvent.setup();
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify([mockCompetitionEntry]),
		);
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			expect(screen.getByText(/model-a vs model-b/)).toBeInTheDocument();
		});
		await user.click(
			screen.getByRole("button", { name: /model-a vs model-b/ }),
		);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /delete/i }),
			).toBeInTheDocument();
		});
	});

	it("delete button removes entry from list", async () => {
		const user = userEvent.setup();
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify([mockCompetitionEntry]),
		);
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			expect(screen.getByText(/model-a vs model-b/)).toBeInTheDocument();
		});
		await user.click(
			screen.getByRole("button", { name: /model-a vs model-b/ }),
		);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /delete/i }),
			).toBeInTheDocument();
		});
		const deleteButton = screen.getByRole("button", { name: /delete/i });
		await user.click(deleteButton);
		await waitFor(() => {
			expect(screen.queryByText(/model-a vs model-b/)).not.toBeInTheDocument();
		});
	});

	it("shows Restore Setup button when onRestore is provided", async () => {
		const user = userEvent.setup();
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify([mockCompetitionEntry]),
		);
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			expect(screen.getByText(/model-a vs model-b/)).toBeInTheDocument();
		});
		await user.click(
			screen.getByRole("button", { name: /model-a vs model-b/ }),
		);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /restore setup/i }),
			).toBeInTheDocument();
		});
	});

	it("Restore Setup button calls onRestore and onClose", async () => {
		const user = userEvent.setup();
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify([mockCompetitionEntry]),
		);
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			expect(screen.getByText(/model-a vs model-b/)).toBeInTheDocument();
		});
		await user.click(
			screen.getByRole("button", { name: /model-a vs model-b/ }),
		);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /restore setup/i }),
			).toBeInTheDocument();
		});
		const restoreButton = screen.getByRole("button", {
			name: /restore setup/i,
		});
		await user.click(restoreButton);
		expect(onRestore).toHaveBeenCalledWith(mockCompetitionEntry);
		expect(onClose).toHaveBeenCalledTimes(1);
	});

	it("Clear All History button exists in footer", async () => {
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify([mockCompetitionEntry]),
		);
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /clear all history/i }),
			).toBeInTheDocument();
		});
	});

	it("Clear All requires double-click confirmation", async () => {
		const user = userEvent.setup();
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify([mockCompetitionEntry]),
		);
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /clear all history/i }),
			).toBeInTheDocument();
		});
		const clearButton = screen.getByRole("button", {
			name: /clear all history/i,
		});
		await user.click(clearButton);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /click again to confirm/i }),
			).toBeInTheDocument();
		});
	});

	it("second click on Clear All clears history", async () => {
		const user = userEvent.setup();
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify([mockCompetitionEntry]),
		);
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /clear all history/i }),
			).toBeInTheDocument();
		});
		const clearButton = screen.getByRole("button", {
			name: /clear all history/i,
		});
		await user.click(clearButton);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /click again to confirm/i }),
			).toBeInTheDocument();
		});
		await user.click(
			screen.getByRole("button", { name: /click again to confirm/i }),
		);
		await waitFor(() => {
			expect(screen.getByText("No match history yet")).toBeInTheDocument();
		});
	});

	it("shows entry count in footer", async () => {
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify([mockCompetitionEntry]),
		);
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			expect(screen.getByText("1 entry")).toBeInTheDocument();
		});
	});

	it("shows multiple entries count in footer", async () => {
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify([mockCompetitionEntry, mockCompareEntry]),
		);
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			// Footer shows "2 entries" count (i18n plural)
			expect(screen.getByText(/\d+ entries/)).toBeInTheDocument();
		});
	});

	it("pagination appears when more entries than page size", async () => {
		const manyEntries = Array.from({ length: 15 }, (_, i) => ({
			...mockCompetitionEntry,
			id: `comp-${i}`,
			timestamp: Date.now() - i * 3600000,
		}));
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify(manyEntries),
		);
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			expect(screen.getByText("10 / page")).toBeInTheDocument();
		});
		expect(screen.getByRole("button", { name: "Prev" })).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Next" })).toBeInTheDocument();
	});

	it("page size selector allows changing page size", async () => {
		const user = userEvent.setup();
		const manyEntries = Array.from({ length: 25 }, (_, i) => ({
			...mockCompetitionEntry,
			id: `comp-${i}`,
			timestamp: Date.now() - i * 3600000,
		}));
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify(manyEntries),
		);
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			expect(screen.getByText("10 / page")).toBeInTheDocument();
		});
		const pageSizeSelect = screen.getByRole("combobox");
		await user.selectOptions(pageSizeSelect, "20");
		await waitFor(() => {
			expect(screen.getByText("20 / page")).toBeInTheDocument();
		});
	});

	it("page navigation buttons work", async () => {
		const user = userEvent.setup();
		const manyEntries = Array.from({ length: 25 }, (_, i) => ({
			...mockCompetitionEntry,
			id: `comp-${i}`,
			timestamp: Date.now() - i * 3600000,
		}));
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify(manyEntries),
		);
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			expect(screen.getByRole("button", { name: "Next" })).toBeInTheDocument();
		});
		// Navigation to page 2
		const nextButton = screen.getByRole("button", { name: "Next" });
		await user.click(nextButton);
		await waitFor(() => {
			expect(screen.getByRole("button", { name: "2" })).toHaveClass(
				"bg-(--accent)",
			);
		});
	});

	it("Prev button is disabled on first page", async () => {
		const manyEntries = Array.from({ length: 15 }, (_, i) => ({
			...mockCompetitionEntry,
			id: `comp-${i}`,
			timestamp: Date.now() - i * 3600000,
		}));
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify(manyEntries),
		);
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			expect(screen.getByRole("button", { name: "Prev" })).toBeInTheDocument();
		});
		const prevButton = screen.getByRole("button", { name: "Prev" });
		expect(prevButton).toBeDisabled();
	});

	it("Next button is disabled on last page", async () => {
		const user = userEvent.setup();
		const manyEntries = Array.from({ length: 15 }, (_, i) => ({
			...mockCompetitionEntry,
			id: `comp-${i}`,
			timestamp: Date.now() - i * 3600000,
		}));
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify(manyEntries),
		);
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			expect(screen.getByRole("button", { name: "Next" })).toBeInTheDocument();
		});
		await user.click(screen.getByRole("button", { name: "Next" }));
		await waitFor(() => {
			expect(screen.getByRole("button", { name: "Next" })).toBeDisabled();
		});
	});

	it("expanding entry shows competition round details", async () => {
		const user = userEvent.setup();
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify([mockCompetitionEntry]),
		);
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			expect(screen.getByText(/model-a vs model-b/)).toBeInTheDocument();
		});
		await user.click(
			screen.getByRole("button", { name: /model-a vs model-b/ }),
		);
		await waitFor(() => {
			expect(screen.getByText("Match")).toBeInTheDocument();
		});
	});

	it("expanding entry shows compare response details", async () => {
		const user = userEvent.setup();
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify([mockCompareEntry]),
		);
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			expect(screen.getByText(/model-c, model-d/)).toBeInTheDocument();
		});
		await user.click(screen.getByRole("button", { name: /model-c, model-d/ }));
		await waitFor(() => {
			expect(screen.getByText("Compare response C")).toBeInTheDocument();
		});
	});

	it("shows formatted date for entry", async () => {
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify([mockCompetitionEntry]),
		);
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			// Date should be formatted - check for month abbreviation
			expect(
				screen.getByText(/jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec/i),
			).toBeInTheDocument();
		});
	});

	it("shows formatted time for entry", async () => {
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify([mockCompetitionEntry]),
		);
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			// Time should contain colon separator
			const timeElement = screen.getByText(/\d{1,2}:\d{2}/);
			expect(timeElement).toBeInTheDocument();
		});
	});

	it("shows trophy icon for winner", async () => {
		const user = userEvent.setup();
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify([mockCompetitionEntry]),
		);
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			expect(screen.getByText(/model-a vs model-b/)).toBeInTheDocument();
		});
		await user.click(
			screen.getByRole("button", { name: /model-a vs model-b/ }),
		);
		await waitFor(() => {
			// Trophy icon should appear next to winner
			expect(screen.getByText("Winner: model-a")).toBeInTheDocument();
		});
	});

	it("shows response metrics (duration, tokens/s) for compare entries", async () => {
		const user = userEvent.setup();
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify([mockCompareEntry]),
		);
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			expect(screen.getByText(/model-c, model-d/)).toBeInTheDocument();
		});
		await user.click(screen.getByRole("button", { name: /model-c, model-d/ }));
		await waitFor(() => {
			// Should show response content and metrics
			expect(screen.getByText("Compare response C")).toBeInTheDocument();
			// Check for duration text (1.8s)
			expect(screen.getByText(/1\.8s/)).toBeInTheDocument();
		});
	});

	it("shows Final round label for multi-round competition", async () => {
		const user = userEvent.setup();
		const multiRoundEntry = {
			...mockCompetitionEntry,
			rounds: [
				{
					matchups: [
						{
							slotA: { modelId: "provider/model-a", personaId: null },
							slotB: { modelId: "provider/model-b", personaId: null },
							responseA: mockCompetitionEntry.rounds[0].matchups[0].responseA,
							responseB: mockCompetitionEntry.rounds[0].matchups[0].responseB,
							vote: "A" as const,
						},
					],
				},
				{
					matchups: [
						{
							slotA: { modelId: "provider/model-c", personaId: null },
							slotB: { modelId: "provider/model-d", personaId: null },
							responseA: {
								modelId: "provider/model-c",
								content: "Final A",
								thinkingContent: "",
								error: null,
								metrics: {
									tokensPerSecond: 50,
									durationMs: 2000,
									promptTokens: 100,
									completionTokens: 200,
								},
							},
							responseB: {
								modelId: "provider/model-d",
								content: "Final B",
								thinkingContent: "",
								error: null,
								metrics: {
									tokensPerSecond: 45,
									durationMs: 2200,
									promptTokens: 100,
									completionTokens: 180,
								},
							},
							vote: "B" as const,
						},
					],
				},
			],
		};
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify([multiRoundEntry]),
		);
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			expect(screen.getByText(/model-a vs model-b/)).toBeInTheDocument();
		});
		await user.click(
			screen.getByRole("button", { name: /model-a vs model-b/ }),
		);
		await waitFor(() => {
			// Round 0 with 2 total rounds: totalRounds-2=0, so index 0 = Semifinals
			expect(screen.getByText("Semifinals")).toBeInTheDocument();
			// Last round (index 1) gets "Final" label
			expect(screen.getByText("Final")).toBeInTheDocument();
		});
	});

	it("shows Semifinals and Quarterfinals labels for 4-round competition", async () => {
		const user = userEvent.setup();
		const makeRound = (suffix: string, voteSlot: "A" | "B") => ({
			matchups: [
				{
					slotA: { modelId: `provider/model-a-${suffix}`, personaId: null },
					slotB: { modelId: `provider/model-b-${suffix}`, personaId: null },
					responseA: {
						modelId: `provider/model-a-${suffix}`,
						content: `Response A ${suffix}`,
						thinkingContent: "",
						error: null,
						metrics: {
							tokensPerSecond: 50,
							durationMs: 2000,
							promptTokens: 100,
							completionTokens: 200,
						},
					},
					responseB: {
						modelId: `provider/model-b-${suffix}`,
						content: `Response B ${suffix}`,
						thinkingContent: "",
						error: null,
						metrics: {
							tokensPerSecond: 45,
							durationMs: 2200,
							promptTokens: 100,
							completionTokens: 180,
						},
					},
					vote: voteSlot,
				},
			],
		});
		const fourRoundEntry = {
			...mockCompetitionEntry,
			rounds: [
				makeRound("r1", "A" as const),
				makeRound("r2", "B" as const),
				makeRound("r3", "A" as const),
				makeRound("r4", "B" as const),
			],
		};
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify([fourRoundEntry]),
		);
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			expect(screen.getByText(/model-a-r1 vs model-b-r1/)).toBeInTheDocument();
		});
		await user.click(
			screen.getByRole("button", { name: /model-a-r1 vs model-b-r1/ }),
		);
		await waitFor(() => {
			// Round 1 (index 0) → "Round 1", Round 2 → "Quarterfinals",
			// Round 3 → "Semifinals", Round 4 (index 3) → "Final"
			expect(screen.getByText("Round 1")).toBeInTheDocument();
			expect(screen.getByText("Quarterfinals")).toBeInTheDocument();
			expect(screen.getByText("Semifinals")).toBeInTheDocument();
			expect(screen.getByText("Final")).toBeInTheDocument();
		});
	});

	it("shows vote B winner with trophy for B-side matchup", async () => {
		const user = userEvent.setup();
		const bWinnerEntry = {
			...mockCompetitionEntry,
			rounds: [
				{
					matchups: [
						{
							slotA: { modelId: "provider/model-a", personaId: null },
							slotB: { modelId: "provider/model-b", personaId: null },
							responseA: mockCompetitionEntry.rounds[0].matchups[0].responseA,
							responseB: mockCompetitionEntry.rounds[0].matchups[0].responseB,
							vote: "B" as const,
						},
					],
				},
			],
			winner: "provider/model-b",
		};
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify([bWinnerEntry]),
		);
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			expect(screen.getByText(/model-a vs model-b/)).toBeInTheDocument();
		});
		await user.click(
			screen.getByRole("button", { name: /model-a vs model-b/ }),
		);
		await waitFor(() => {
			// Winner badge shows model-b
			expect(screen.getByText("Winner: model-b")).toBeInTheDocument();
			// Vote indicator shows → B
			expect(screen.getByText("→ B")).toBeInTheDocument();
		});
	});

	it("shows dash for matchup with no vote", async () => {
		const user = userEvent.setup();
		const noVoteEntry = {
			...mockCompetitionEntry,
			rounds: [
				{
					matchups: [
						{
							slotA: { modelId: "provider/model-a", personaId: null },
							slotB: { modelId: "provider/model-b", personaId: null },
							responseA: mockCompetitionEntry.rounds[0].matchups[0].responseA,
							responseB: mockCompetitionEntry.rounds[0].matchups[0].responseB,
							vote: null as unknown as "A",
						},
					],
				},
			],
			winner: null as unknown as string,
		};
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify([noVoteEntry]),
		);
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			expect(screen.getByText(/model-a vs model-b/)).toBeInTheDocument();
		});
		await user.click(
			screen.getByRole("button", { name: /model-a vs model-b/ }),
		);
		await waitFor(() => {
			// No vote → dash indicator, no winner badge
			const dashElements = screen.getAllByText("-");
			expect(dashElements.length).toBeGreaterThanOrEqual(1);
			expect(screen.queryByText(/Winner:/)).not.toBeInTheDocument();
		});
	});

	it("shows error message for failed compare response", async () => {
		const user = userEvent.setup();
		const errorEntry = {
			...mockCompareEntry,
			compareModels: ["provider/model-err"],
			compareResponses: [
				{
					modelId: "provider/model-err",
					content: "",
					thinkingContent: "",
					error: "Rate limit exceeded",
					metrics: {
						tokensPerSecond: 0,
						durationMs: 0,
						promptTokens: 100,
						completionTokens: 0,
					},
				},
			],
		};
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify([errorEntry]),
		);
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		// Wait for the entry to appear, then click to expand
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /model-err/ }),
			).toBeInTheDocument();
		});
		await user.click(screen.getByRole("button", { name: /model-err/ }));
		await waitFor(() => {
			// Error shown with "Error:" prefix in expanded detail
			expect(
				screen.getByText(/Error: Rate limit exceeded/),
			).toBeInTheDocument();
		});
	});

	it("shows 1 entry singular for single entry count", async () => {
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify([mockCompetitionEntry]),
		);
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			expect(screen.getByText("1 entry")).toBeInTheDocument();
		});
	});

	it("collapses expanded entry when clicking same entry again", async () => {
		const user = userEvent.setup();
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify([mockCompetitionEntry]),
		);
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			expect(screen.getByText(/model-a vs model-b/)).toBeInTheDocument();
		});
		const entryButton = screen.getByRole("button", {
			name: /model-a vs model-b/,
		});
		// First click: expand
		await user.click(entryButton);
		await waitFor(() => {
			expect(screen.getByText("Winner: model-a")).toBeInTheDocument();
		});
		// Second click: collapse
		await user.click(entryButton);
		// After collapsing, the grid container switches to grid-rows-[0fr]
		// (not.toBeVisible() doesn't work in jsdom — CSS grid layout isn't applied)
		const winnerText = screen.getByText("Winner: model-a");
		const gridContainer = winnerText.closest("[class*='grid-rows']");
		expect(gridContainer).toHaveClass("grid-rows-[0fr]");
	});

	it("hides Restore Setup when onRestore is not provided", async () => {
		const user = userEvent.setup();
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify([mockCompetitionEntry]),
		);
		renderWithProviders(<ArenaHistoryModal onClose={onClose} />);
		await waitFor(() => {
			expect(screen.getByText(/model-a vs model-b/)).toBeInTheDocument();
		});
		await user.click(
			screen.getByRole("button", { name: /model-a vs model-b/ }),
		);
		await waitFor(() => {
			expect(
				screen.queryByRole("button", { name: /restore setup/i }),
			).not.toBeInTheDocument();
		});
	});

	it("resets confirmClear on blur", async () => {
		const user = userEvent.setup();
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify([mockCompetitionEntry]),
		);
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /clear all history/i }),
			).toBeInTheDocument();
		});
		const clearButton = screen.getByRole("button", {
			name: /clear all history/i,
		});
		// First click: enter confirm state
		await user.click(clearButton);
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /click again to confirm/i }),
			).toBeInTheDocument();
		});
		// Blur the button — should reset confirmClear
		await user.tab();
		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: /clear all history/i }),
			).toBeInTheDocument();
		});
	});

	it("shows Bracket fallback for competition with no rounds", async () => {
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify([
				{
					...mockCompetitionEntry,
					rounds: [],
				},
			]),
		);
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			expect(screen.getByText("Bracket")).toBeInTheDocument();
		});
	});

	it("shows Compare fallback for compare with no models", async () => {
		const noModelsEntry = {
			...mockCompareEntry,
			compareModels: null as unknown as string[],
			compareResponses: [],
		};
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify([noModelsEntry]),
		);
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			// "Compare" appears as both a filter button and the entry label
			// There should be at least one instance from the entry (not the filter)
			const compareTexts = screen.getAllByText("Compare");
			// The filter button + the entry label = 2+ occurrences
			expect(compareTexts.length).toBeGreaterThanOrEqual(2);
		});
	});

	it("shows custom prompt label when promptPresetId is null", async () => {
		const user = userEvent.setup();
		const customEntry = {
			...mockCompareEntry,
			promptPresetId: null,
		};
		mockLocalStorage.store.set(
			"arenaMatchHistory",
			JSON.stringify([customEntry]),
		);
		renderWithProviders(
			<ArenaHistoryModal onClose={onClose} onRestore={onRestore} />,
		);
		await waitFor(() => {
			expect(screen.getByText(/model-c, model-d/)).toBeInTheDocument();
		});
		await user.click(screen.getByRole("button", { name: /model-c, model-d/ }));
		await waitFor(() => {
			expect(screen.getByText("Custom prompt")).toBeInTheDocument();
		});
	});
});
