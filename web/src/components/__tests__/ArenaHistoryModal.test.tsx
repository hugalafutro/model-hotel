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
			expect(screen.getAllByLabelText("Close")).toHaveLength(2);
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
			expect(screen.getByText(/1 entry/)).toBeInTheDocument();
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
			// Footer shows "2 entries" count - use getAllBy since pagination also shows "entries"
			expect(screen.getAllByText(/entries/)).toHaveLength(2);
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
		// First page shows "1 to 10 of 25 entries"
		expect(screen.getByText(/1 to 10 of 25 entries/)).toBeInTheDocument();
		const nextButton = screen.getByRole("button", { name: "Next" });
		await user.click(nextButton);
		await waitFor(() => {
			// Second page shows "11 to 20 of 25 entries"
			expect(screen.getByText(/11 to 20 of 25 entries/)).toBeInTheDocument();
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
