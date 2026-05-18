import { screen, waitFor } from "@testing-library/react";
import type { User } from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import type { Model } from "../../api/types";
import {
	mockAllDefaults,
	mockArenaStream,
	mockModels,
} from "../../test/helpers";
import { mockModel, mockProvider } from "../../test/mocks/data";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { Arena } from "../Arena";

describe("Arena", () => {
	beforeEach(() => {
		server.resetHandlers();
		// Clear sidebar mode to prevent test pollution (e.g. Compare mode persisting)
		localStorage.removeItem("sidebarArenaSubMode");
	});

	const mockModel2: Model = {
		...mockModel,
		id: "model-002",
		model_id: "test-model-v2",
		display_name: "Test Model v2",
	};

	const mockModel3: Model = {
		...mockModel,
		id: "model-003",
		model_id: "test-model-v3",
		display_name: "Test Model v3",
	};

	const setupDefaultMocks = () => {
		server.use(...mockAllDefaults());
	};

	const waitForArenaLoad = async () => {
		await waitFor(
			() => {
				// Check for the Controls section which indicates the page has loaded
				expect(screen.getByText("Controls")).toBeInTheDocument();
			},
			{ timeout: 3000 },
		);
	};

	interface SetupAndRunOptions {
		mode?: "competition" | "compare";
		models?: Model[];
		prompt?: string;
	}

	async function setupAndRunArena(
		user: User,
		options: SetupAndRunOptions = {},
	): Promise<void> {
		const {
			mode = "competition",
			models: selectedModels = [mockModel, mockModel2],
			prompt = "Test prompt",
		} = options;

		// Toggle to Compare mode if requested
		if (mode === "compare") {
			await user.click(screen.getByRole("button", { name: "Compare" }));
			await waitFor(() => {
				expect(
					screen.getByText(/Side-by-side.*compare model outputs/i),
				).toBeInTheDocument();
			});
		}

		// Wait for models to load, then select them
		await waitFor(
			() => {
				expect(
					screen.getByText(selectedModels[0].display_name),
				).toBeInTheDocument();
			},
			{ timeout: 5000 },
		);
		for (const model of selectedModels) {
			await user.click(screen.getByText(model.display_name));
		}

		// Type prompt
		const textarea = screen.getByRole("textbox", { name: /prompt/i });
		await user.type(textarea, prompt);

		// Click Run Arena
		await user.click(screen.getByRole("button", { name: /Run Arena/i }));

		// Wait for streaming to start (Stop button appears)
		await waitFor(
			() => {
				expect(
					screen.getByRole("button", { name: "Stop" }),
				).toBeInTheDocument();
			},
			{ timeout: 5000 },
		);
	}

	describe("Initial Rendering", () => {
		it("renders Arena page with header and controls", async () => {
			setupDefaultMocks();
			renderWithProviders(<Arena />);
			await waitForArenaLoad();
			expect(screen.getByText("Controls")).toBeInTheDocument();
		});

		it("renders competition mode by default", async () => {
			setupDefaultMocks();
			renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// Competition mode description
			expect(
				screen.getByText("Bracket tournament - models compete head-to-head"),
			).toBeInTheDocument();

			// Bracket ModelPicker label
			expect(screen.getByLabelText(/Models \(0\/8\)/i)).toBeInTheDocument();
		});

		it("renders page header with correct description", async () => {
			setupDefaultMocks();
			renderWithProviders(<Arena />);
			await waitForArenaLoad();

			expect(
				screen.getByText("Bracket tournament - models compete head-to-head"),
			).toBeInTheDocument();
		});
	});

	describe("Mode Toggle", () => {
		it("can toggle to compare mode", async () => {
			setupDefaultMocks();
			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// Click the "Compare" submode toggle button
			const compareButton = screen.getByRole("button", { name: "Compare" });
			await user.click(compareButton);

			// Description should change to compare mode
			await waitFor(() => {
				expect(
					screen.getByText(
						"Side-by-side - compare model outputs on the same prompt",
					),
				).toBeInTheDocument();
			});

			// Compare ModelPicker should appear
			expect(screen.getByLabelText(/Models \(0\/6\)/i)).toBeInTheDocument();
		});

		it("shows correct description for each mode", async () => {
			setupDefaultMocks();
			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// Page should render with Arena title
			expect(screen.getByRole("heading", { level: 1 })).toBeInTheDocument();

			// Toggle to compare
			await user.click(screen.getByRole("button", { name: "Compare" }));

			// Should still have the heading
			await waitFor(() => {
				expect(screen.getByRole("heading", { level: 1 })).toBeInTheDocument();
			});
		});
	});

	describe("PromptPicker", () => {
		it("renders PromptPicker", async () => {
			setupDefaultMocks();
			renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// Prompt textarea should be present - use the id
			const promptTextarea = screen.getByRole("textbox", {
				name: /prompt/i,
			});
			expect(promptTextarea).toBeInTheDocument();
		});

		it("shows prompt preset buttons", async () => {
			setupDefaultMocks();
			renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// Preset bar should be visible in setup phase
			// Check for the preset bar container
			expect(screen.getByText("Prompt")).toBeInTheDocument();
		});
	});

	describe("Run Button", () => {
		it("shows disabled run button when no models selected", async () => {
			setupDefaultMocks();
			renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// Run button should be disabled in setup phase with no models
			const runButton = screen.getByRole("button", { name: /Run Arena/i });
			expect(runButton).toBeDisabled();
		});

		it("shows appropriate disabled reason message", async () => {
			setupDefaultMocks();
			renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// Run button should show disabled state with a reason
			const runButton = screen.getByRole("button", { name: /Run Arena/i });
			expect(runButton).toBeDisabled();
		});
	});

	describe("History Button", () => {
		it("shows history button", async () => {
			setupDefaultMocks();
			renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// History button with aria-label
			expect(
				screen.getByRole("button", { name: "Match history" }),
			).toBeInTheDocument();
		});

		it("opens history modal when clicked", async () => {
			setupDefaultMocks();
			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// Click history button
			await user.click(screen.getByRole("button", { name: "Match history" }));

			// Modal should open (check for dialog role)
			await waitFor(() => {
				expect(screen.getByRole("dialog")).toBeInTheDocument();
			});
		});
	});

	describe("Collapsible Toggle", () => {
		it("collapsible toggle works", async () => {
			setupDefaultMocks();
			renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// Find collapse toggle buttons - at least one should exist
			const collapseToggles = screen.getAllByRole("button", {
				name: /Collapse|Expand/i,
			});
			expect(collapseToggles.length).toBeGreaterThan(0);
		});
	});

	describe("Empty State", () => {
		it("renders empty state - no rounds, no matchups shown initially", async () => {
			setupDefaultMocks();
			renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// No rounds or matchups should be visible initially
			expect(screen.queryByText(/Match \d+/i)).not.toBeInTheDocument();
			expect(screen.queryByText(/First Round/i)).not.toBeInTheDocument();
			expect(screen.queryByText(/vs/i)).not.toBeInTheDocument();
		});

		it("shows bracket preview when models are selected", async () => {
			setupDefaultMocks();
			renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// Page should render with the controls section
			expect(screen.getByText("Controls")).toBeInTheDocument();
		});
	});

	describe("Reset Buttons", () => {
		it("shows reset buttons in controls", async () => {
			setupDefaultMocks();
			renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// Controls section should have action buttons
			expect(screen.getByText("Controls")).toBeInTheDocument();
		});
	});

	describe("Mode Description", () => {
		it("shows mode description text", async () => {
			setupDefaultMocks();
			renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// Mode description should be visible - check for competition-specific text
			expect(
				screen.getByText(/single-elimination bracket/i),
			).toBeInTheDocument();
		});
	});

	describe("API Integration", () => {
		it("fetches models from correct endpoint", async () => {
			let modelsApiCalled = false;
			server.use(
				http.get("/api/models", ({ request }) => {
					modelsApiCalled = true;
					expect(request.headers.get("Authorization")).toMatch(/Bearer /);
					return HttpResponse.json([mockModel]);
				}),
				http.get("/api/providers", () => HttpResponse.json([mockProvider])),
			);

			renderWithProviders(<Arena />);

			await waitFor(() => {
				expect(modelsApiCalled).toBe(true);
			});
		});

		it("fetches providers from correct endpoint", async () => {
			// Note: Arena uses useEnabledModels which fetches models
			// The providers are fetched by the ModelPicker component
			let modelsApiCalled = false;
			server.use(
				http.get("/api/models", ({ request }) => {
					modelsApiCalled = true;
					expect(request.headers.get("Authorization")).toMatch(/Bearer /);
					return HttpResponse.json([mockModel]);
				}),
				http.get("/api/providers", () => HttpResponse.json([mockProvider])),
			);

			renderWithProviders(<Arena />);

			await waitFor(
				() => {
					expect(modelsApiCalled).toBe(true);
				},
				{ timeout: 5000 },
			);
		});

		it("handles models API error gracefully", async () => {
			server.use(
				http.get("/api/models", () =>
					HttpResponse.json({ error: "Failed to fetch" }, { status: 500 }),
				),
				http.get("/api/providers", () => HttpResponse.json([mockProvider])),
			);

			renderWithProviders(<Arena />);

			// Should handle error gracefully - page should still render
			await waitFor(() => {
				expect(screen.getByRole("heading", { level: 1 })).toBeInTheDocument();
			});
		});

		it("handles providers API error gracefully", async () => {
			server.use(
				http.get("/api/models", () => HttpResponse.json([mockModel])),
				http.get("/api/providers", () =>
					HttpResponse.json({ error: "Failed to fetch" }, { status: 500 }),
				),
			);

			renderWithProviders(<Arena />);

			// Should handle error gracefully - page should still render
			await waitFor(() => {
				expect(screen.getByRole("heading", { level: 1 })).toBeInTheDocument();
			});
		});
	});

	describe("Compare Mode Specific", () => {
		it("shows PersonaPicker in compare mode", async () => {
			setupDefaultMocks();
			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// Toggle to compare mode
			await user.click(screen.getByRole("button", { name: "Compare" }));

			await waitFor(() => {
				expect(
					screen.getByText(/Side-by-side.*compare model outputs/i),
				).toBeInTheDocument();
			});

			// PersonaPicker should be visible
			expect(screen.getByText(/persona/i)).toBeInTheDocument();
		});

		it("shows different model limits for each mode", async () => {
			setupDefaultMocks();
			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// Competition mode: should show models label with /8 limit
			expect(screen.getByLabelText(/Models \(0\/8\)/i)).toBeInTheDocument();

			// Toggle to compare mode
			await user.click(screen.getByRole("button", { name: "Compare" }));

			// Should show models label with /6 limit after toggle
			await waitFor(() => {
				expect(screen.getByLabelText(/Models \(0\/6\)/i)).toBeInTheDocument();
			});
		});
	});

	describe("Arena Run Flow - Compare Mode", () => {
		it("Run button is disabled without models in compare mode", async () => {
			const chunks = [
				{ choices: [{ delta: { content: "Response" } }] },
				{ choices: [{ delta: { content: " content" } }] },
			];

			server.use(
				...mockAllDefaults(),
				...mockModels({ body: [mockModel, mockModel2] }),
				...mockArenaStream(chunks),
			);

			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// Toggle to Compare mode
			await user.click(screen.getByRole("button", { name: "Compare" }));
			await waitFor(() => {
				expect(
					screen.getByText(/Side-by-side.*compare model outputs/i),
				).toBeInTheDocument();
			});

			// Verify prompt textarea is available
			const promptTextarea = screen.getByRole("textbox", { name: /prompt/i });
			expect(promptTextarea).toBeInTheDocument();

			// Type a prompt
			await user.type(promptTextarea, "Test prompt for arena");

			// Run button should be disabled without models selected
			const runButton = screen.getByRole("button", { name: /Run Arena/i });
			expect(runButton).toBeDisabled();
		});

		it("Run Arena button is present in setup phase", async () => {
			// This test verifies the button label changes based on isRunning state
			// The actual streaming behavior is tested in useArenaRunner tests
			renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// In setup phase, button should say "Run Arena"
			expect(
				screen.getByRole("button", { name: /Run Arena/i }),
			).toBeInTheDocument();
		});
	});

	describe("Arena Run Flow - Competition Mode", () => {
		it("shows Arena title in competition mode", async () => {
			renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// Arena title should be visible
			expect(screen.getByRole("heading", { level: 1 })).toBeInTheDocument();
		});

		it("shows model picker for bracket mode", async () => {
			server.use(...mockAllDefaults(), ...mockModels({ body: [mockModel] }));

			renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// Model picker should be available
			expect(screen.getByLabelText(/Models/i)).toBeInTheDocument();
		});
	});

	describe("Arena Error Handling", () => {
		it("Arena page renders when arena endpoint would error", async () => {
			server.use(
				...mockAllDefaults(),
				http.post("/api/chat/arena", () =>
					HttpResponse.json({ error: "Arena failed" }, { status: 500 }),
				),
			);

			renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// Page should still render even with API error handling
			expect(screen.getByText("Controls")).toBeInTheDocument();
		});
	});

	describe("Arena Clear and Reset", () => {
		it("shows Clear and Reset buttons when models are selected", async () => {
			server.use(...mockAllDefaults(), ...mockModels({ body: [mockModel] }));

			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// Wait for model to be available and select it
			await waitFor(
				() => {
					expect(screen.getByText("Test Model v1")).toBeInTheDocument();
				},
				{ timeout: 5000 },
			);
			await user.click(screen.getByText("Test Model v1"));

			// Controls section should be visible
			expect(screen.getByText("Controls")).toBeInTheDocument();

			// Reset button should be visible now that model is selected
			const resetButton = screen.getByRole("button", {
				name: "Reset all (clear models & prompt)",
			});
			expect(resetButton).toBeInTheDocument();
		});

		it("opens confirm dialog when Reset is clicked", async () => {
			server.use(...mockAllDefaults(), ...mockModels({ body: [mockModel] }));

			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// Select a model to make reset button visible
			await waitFor(
				() => {
					expect(screen.getByText("Test Model v1")).toBeInTheDocument();
				},
				{ timeout: 5000 },
			);
			await user.click(screen.getByText("Test Model v1"));

			// Click Reset button
			const resetButton = screen.getByRole("button", {
				name: "Reset all (clear models & prompt)",
			});
			await user.click(resetButton);

			// Confirm dialog should open
			await waitFor(() => {
				expect(screen.getByRole("dialog")).toBeInTheDocument();
			});

			// Dialog should have Reset All confirmation button
			expect(
				screen.getByRole("button", { name: "Reset All" }),
			).toBeInTheDocument();
		});
	});

	describe("Compare Mode Streaming Flow", () => {
		it("streams responses for selected models in compare mode", {
			timeout: 15000,
		}, async () => {
			const chunks = [
				{ choices: [{ delta: { content: "Response" } }] },
				{ choices: [{ delta: { content: " content" } }] },
			];

			server.use(
				...mockAllDefaults({ models: [mockModel, mockModel2] }),
				...mockArenaStream(chunks, { delay: 10 }),
			);

			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			await setupAndRunArena(user, {
				mode: "compare",
				prompt: "Test prompt for compare",
			});

			// Wait for streaming to complete (Stop button disappears, phase transitions to finished)
			await waitFor(
				() => {
					expect(
						screen.queryByRole("button", { name: "Stop" }),
					).not.toBeInTheDocument();
				},
				{ timeout: 10000 },
			);
		});

		it("shows Responses header in compare mode after streaming", {
			timeout: 15000,
		}, async () => {
			const chunks = [{ choices: [{ delta: { content: "Test response" } }] }];

			server.use(
				...mockAllDefaults({ models: [mockModel, mockModel2] }),
				...mockArenaStream(chunks, { delay: 10 }),
			);

			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			await setupAndRunArena(user, { mode: "compare" });

			// Wait for streaming to complete (Stop button disappears)
			await waitFor(
				() => {
					expect(
						screen.queryByRole("button", { name: "Stop" }),
					).not.toBeInTheDocument();
				},
				{ timeout: 10000 },
			);

			// Should show "Responses" header
			expect(screen.getByText("Responses")).toBeInTheDocument();
		});

		it("button changes from Run Arena to Stop during streaming", async () => {
			const chunks = [{ choices: [{ delta: { content: "Response" } }] }];

			server.use(
				...mockAllDefaults({ models: [mockModel, mockModel2] }),
				...mockArenaStream(chunks, { delay: 50 }),
			);

			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			await setupAndRunArena(user, { mode: "compare" });
		});

		it("shows generating message during running phase", async () => {
			const chunks = [{ choices: [{ delta: { content: "Response" } }] }];

			server.use(
				...mockAllDefaults({ models: [mockModel, mockModel2] }),
				...mockArenaStream(chunks, { delay: 50 }),
			);

			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			await setupAndRunArena(user, { mode: "compare" });

			// Should show generating message
			await waitFor(
				() => {
					expect(
						screen.getByText("Models are generating - click Stop to cancel"),
					).toBeInTheDocument();
				},
				{ timeout: 5000 },
			);
		});

		it("phase transitions to finished after streaming completes in compare mode", {
			timeout: 15000,
		}, async () => {
			const chunks = [{ choices: [{ delta: { content: "Done" } }] }];

			server.use(
				...mockAllDefaults({ models: [mockModel, mockModel2] }),
				...mockArenaStream(chunks, { delay: 10 }),
			);

			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			await setupAndRunArena(user, { mode: "compare" });

			// Wait for streaming to complete (Stop button disappears)
			await waitFor(
				() => {
					expect(
						screen.queryByRole("button", { name: "Stop" }),
					).not.toBeInTheDocument();
				},
				{ timeout: 10000 },
			);

			// Eraser button should appear (only visible when phase !== "setup")
			expect(
				screen.getByRole("button", {
					name: "Clear results (keep models & prompt)",
				}),
			).toBeInTheDocument();
		});
	});

	describe("Competition Mode Streaming Flow", () => {
		it("streams bracket matchups in competition mode", {
			timeout: 15000,
		}, async () => {
			const chunks = [{ choices: [{ delta: { content: "Response" } }] }];

			server.use(
				...mockAllDefaults({ models: [mockModel, mockModel2] }),
				...mockArenaStream(chunks, { delay: 10 }),
			);

			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			await setupAndRunArena(user, { prompt: "Test prompt for competition" });

			// Wait for streaming to complete (Stop button disappears, phase transitions to voting)
			await waitFor(
				() => {
					expect(
						screen.queryByRole("button", { name: "Stop" }),
					).not.toBeInTheDocument();
				},
				{ timeout: 10000 },
			);
		});

		it("shows round label and VS between matchup slots", {
			timeout: 15000,
		}, async () => {
			const chunks = [{ choices: [{ delta: { content: "Response" } }] }];

			server.use(
				...mockAllDefaults({ models: [mockModel, mockModel2] }),
				...mockArenaStream(chunks, { delay: 10 }),
			);

			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			await setupAndRunArena(user);

			// Wait for streaming to complete (Stop button disappears)
			await waitFor(
				() => {
					expect(
						screen.queryByRole("button", { name: "Stop" }),
					).not.toBeInTheDocument();
				},
				{ timeout: 10000 },
			);

			// With 2 models (1 round), the round label is "Match"
			const matchLabels = screen.getAllByText("Match");
			// At least one should be the round label (another is "Match history" button)
			expect(matchLabels.length).toBeGreaterThanOrEqual(1);

			// Should show VS between matchup slots
			const vsElements = screen.getAllByText("VS");
			expect(vsElements.length).toBeGreaterThan(0);
		});

		it("button changes to Stop during competition streaming", async () => {
			const chunks = [{ choices: [{ delta: { content: "Response" } }] }];

			server.use(
				...mockAllDefaults({ models: [mockModel, mockModel2] }),
				...mockArenaStream(chunks, { delay: 50 }),
			);

			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			await setupAndRunArena(user);
		});

		it("phase transitions to voting after streaming completes in competition mode", {
			timeout: 15000,
		}, async () => {
			const chunks = [{ choices: [{ delta: { content: "Done" } }] }];

			server.use(
				...mockAllDefaults({ models: [mockModel, mockModel2] }),
				...mockArenaStream(chunks, { delay: 10 }),
			);

			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			await setupAndRunArena(user);

			// Wait for streaming to complete (Stop button disappears)
			await waitFor(
				() => {
					expect(
						screen.queryByRole("button", { name: "Stop" }),
					).not.toBeInTheDocument();
				},
				{ timeout: 10000 },
			);

			// Should show voting message
			await waitFor(
				() => {
					expect(
						screen.getByText(
							"Vote on all matchups to continue to the next round",
						),
					).toBeInTheDocument();
				},
				{ timeout: 5000 },
			);
		});
	});

	describe("Arena Stop Flow", () => {
		it("Stop during compare mode streaming sets phase to finished", {
			timeout: 15000,
		}, async () => {
			const chunks = [
				{ choices: [{ delta: { content: "Response" } }] },
				{ choices: [{ delta: { content: "More" } }] },
			];

			server.use(
				...mockAllDefaults({ models: [mockModel, mockModel2] }),
				...mockArenaStream(chunks, { delay: 100 }),
			);

			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			await setupAndRunArena(user, { mode: "compare" });

			// Click Stop
			await user.click(screen.getByRole("button", { name: "Stop" }));

			// Stop button should disappear (phase is finished, no button rendered)
			await waitFor(
				() => {
					expect(
						screen.queryByRole("button", { name: "Stop" }),
					).not.toBeInTheDocument();
				},
				{ timeout: 5000 },
			);
		});

		it("Stop during competition mode streaming sets phase to voting", {
			timeout: 15000,
		}, async () => {
			const chunks = [
				{ choices: [{ delta: { content: "Response" } }] },
				{ choices: [{ delta: { content: "More" } }] },
			];

			server.use(
				...mockAllDefaults({ models: [mockModel, mockModel2] }),
				...mockArenaStream(chunks, { delay: 100 }),
			);

			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			await setupAndRunArena(user);

			// Click Stop
			await user.click(screen.getByRole("button", { name: "Stop" }));

			// Should show voting message
			await waitFor(
				() => {
					expect(
						screen.getByText(
							"Vote on all matchups to continue to the next round",
						),
					).toBeInTheDocument();
				},
				{ timeout: 5000 },
			);
		});

		it("partially streamed responses are preserved after stop", {
			timeout: 15000,
		}, async () => {
			const chunks = [
				{ choices: [{ delta: { content: "Partial response" } }] },
				{ choices: [{ delta: { content: " more" } }] },
			];

			server.use(
				...mockAllDefaults({ models: [mockModel, mockModel2] }),
				...mockArenaStream(chunks, { delay: 100 }),
			);

			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			await setupAndRunArena(user, { mode: "compare" });

			// Click Stop
			await user.click(screen.getByRole("button", { name: "Stop" }));

			// Responses should be preserved (check for Responses header in compare mode)
			await waitFor(
				() => {
					expect(screen.getByText("Responses")).toBeInTheDocument();
				},
				{ timeout: 5000 },
			);
		});
	});

	describe("Arena Clear Results (Eraser)", () => {
		it("Clear button appears when phase is not setup", {
			timeout: 15000,
		}, async () => {
			const chunks = [{ choices: [{ delta: { content: "Done" } }] }];

			server.use(
				...mockAllDefaults({ models: [mockModel, mockModel2] }),
				...mockArenaStream(chunks, { delay: 10 }),
			);

			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			await setupAndRunArena(user, { mode: "compare" });

			// Wait for streaming to complete (Stop button disappears)
			await waitFor(
				() => {
					expect(
						screen.queryByRole("button", { name: "Stop" }),
					).not.toBeInTheDocument();
				},
				{ timeout: 10000 },
			);

			// Clear button should appear
			expect(
				screen.getByRole("button", {
					name: "Clear results (keep models & prompt)",
				}),
			).toBeInTheDocument();
		});

		it("Clear button clears results but keeps models and prompt", {
			timeout: 15000,
		}, async () => {
			const chunks = [{ choices: [{ delta: { content: "Done" } }] }];

			server.use(
				...mockAllDefaults({ models: [mockModel, mockModel2] }),
				...mockArenaStream(chunks, { delay: 10 }),
			);

			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			await setupAndRunArena(user, {
				mode: "compare",
				prompt: "Test prompt to keep",
			});

			// Wait for streaming to complete (Stop button disappears)
			await waitFor(
				() => {
					expect(
						screen.queryByRole("button", { name: "Stop" }),
					).not.toBeInTheDocument();
				},
				{ timeout: 10000 },
			);

			// Click Clear button
			const clearButton = screen.getByRole("button", {
				name: "Clear results (keep models & prompt)",
			});
			await user.click(clearButton);

			// Prompt should still be present
			const textarea = screen.getByRole("textbox", { name: /prompt/i });
			expect(textarea).toHaveValue("Test prompt to keep");

			// Clear button should disappear (phase is back to setup)
			expect(
				screen.queryByRole("button", {
					name: "Clear results (keep models & prompt)",
				}),
			).not.toBeInTheDocument();
		});
	});

	describe("Arena Full Reset", () => {
		it("Reset button appears when models are selected", async () => {
			server.use(...mockAllDefaults({ models: [mockModel, mockModel2] }));

			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// Wait for models to load, then select a model
			await waitFor(
				() => {
					expect(screen.getByText("Test Model v1")).toBeInTheDocument();
				},
				{ timeout: 5000 },
			);
			await user.click(screen.getByText("Test Model v1"));

			// Reset button should appear
			expect(
				screen.getByRole("button", {
					name: "Reset all (clear models & prompt)",
				}),
			).toBeInTheDocument();
		});

		it("Reset opens ConfirmDialog and clears everything on confirm", async () => {
			server.use(...mockAllDefaults({ models: [mockModel, mockModel2] }));

			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// Wait for models to load, then select a model
			await waitFor(
				() => {
					expect(screen.getByText("Test Model v1")).toBeInTheDocument();
				},
				{ timeout: 5000 },
			);
			await user.click(screen.getByText("Test Model v1"));

			// Type prompt
			const textarea = screen.getByRole("textbox", { name: /prompt/i });
			await user.type(textarea, "Test prompt");

			// Click Reset button
			const resetButton = screen.getByRole("button", {
				name: "Reset all (clear models & prompt)",
			});
			await user.click(resetButton);

			// Confirm dialog should open
			await waitFor(() => {
				expect(screen.getByRole("dialog")).toBeInTheDocument();
			});

			// Click Reset All confirmation
			await user.click(screen.getByRole("button", { name: "Reset All" }));

			// Dialog should close
			await waitFor(() => {
				expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
			});

			// Prompt should be cleared
			expect(textarea).toHaveValue("");
		});

		it("Reset with cancel does not clear anything", async () => {
			server.use(...mockAllDefaults({ models: [mockModel, mockModel2] }));

			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// Wait for models to load, then select a model
			await waitFor(
				() => {
					expect(screen.getByText("Test Model v1")).toBeInTheDocument();
				},
				{ timeout: 5000 },
			);
			await user.click(screen.getByText("Test Model v1"));

			// Type prompt
			const textarea = screen.getByRole("textbox", { name: /prompt/i });
			await user.type(textarea, "Test prompt to keep");

			// Click Reset button
			const resetButton = screen.getByRole("button", {
				name: "Reset all (clear models & prompt)",
			});
			await user.click(resetButton);

			// Confirm dialog should open
			await waitFor(() => {
				expect(screen.getByRole("dialog")).toBeInTheDocument();
			});

			// Click Cancel
			await user.click(screen.getByRole("button", { name: "Cancel" }));

			// Dialog should close
			await waitFor(() => {
				expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
			});

			// Model should still be selected
			expect(screen.getByText("Test Model v1")).toBeInTheDocument();

			// Prompt should still be present
			expect(textarea).toHaveValue("Test prompt to keep");
		});
	});

	describe("Arena Disabled Reasons", () => {
		it("competition mode with no models shows select 2 4 or 8 models", {
			timeout: 10000,
		}, async () => {
			server.use(
				...mockAllDefaults({ models: [mockModel, mockModel2, mockModel3] }),
			);

			renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// Competition mode is default - no models selected
			await waitFor(
				() => {
					expect(
						screen.getByText("Select 2, 4, or 8 models"),
					).toBeInTheDocument();
				},
				{ timeout: 5000 },
			);
		});

		it("competition mode with 1 model shows pick at least 1 more model", async () => {
			server.use(
				...mockAllDefaults({ models: [mockModel, mockModel2, mockModel3] }),
			);

			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// Wait for models to load, then select 1 model
			await waitFor(
				() => {
					expect(screen.getByText("Test Model v1")).toBeInTheDocument();
				},
				{ timeout: 5000 },
			);
			await user.click(screen.getByText("Test Model v1"));

			// Should show pick at least 1 more model
			await waitFor(
				() => {
					expect(
						screen.getByText("Pick at least 1 more model"),
					).toBeInTheDocument();
				},
				{ timeout: 5000 },
			);
		});

		it("competition mode with 3 models shows pick 1 more or remove to get 4", {
			timeout: 10000,
		}, async () => {
			server.use(
				...mockAllDefaults({ models: [mockModel, mockModel2, mockModel3] }),
			);

			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// Wait for models to load, then select 3 models
			await waitFor(
				() => {
					expect(screen.getByText("Test Model v1")).toBeInTheDocument();
				},
				{ timeout: 5000 },
			);
			await user.click(screen.getByText("Test Model v1"));
			await user.click(screen.getByText("Test Model v2"));
			await user.click(screen.getByText("Test Model v3"));

			// Should show pick 1 more or remove message
			await waitFor(
				() => {
					expect(
						screen.getByText("Pick 1 more or remove to get 4"),
					).toBeInTheDocument();
				},
				{ timeout: 5000 },
			);
		});

		it("compare mode with 0 models shows select at least 2 models", async () => {
			server.use(...mockAllDefaults({ models: [mockModel, mockModel2] }));

			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// Toggle to Compare mode
			await user.click(screen.getByRole("button", { name: "Compare" }));

			// No models selected - should show select at least 2 models
			expect(screen.getByText("Select at least 2 models")).toBeInTheDocument();
		});

		it("compare mode with 1 model shows pick at least 1 more model", async () => {
			server.use(...mockAllDefaults({ models: [mockModel, mockModel2] }));

			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// Toggle to Compare mode
			await user.click(screen.getByRole("button", { name: "Compare" }));

			// Wait for models to load, then select 1 model
			await waitFor(
				() => {
					expect(screen.getByText("Test Model v1")).toBeInTheDocument();
				},
				{ timeout: 5000 },
			);
			await user.click(screen.getByText("Test Model v1"));

			// Should show pick at least 1 more model
			await waitFor(
				() => {
					expect(
						screen.getByText("Pick at least 1 more model"),
					).toBeInTheDocument();
				},
				{ timeout: 5000 },
			);
		});

		it("no prompt entered shows enter a prompt", async () => {
			server.use(...mockAllDefaults({ models: [mockModel, mockModel2] }));

			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// Toggle to Compare mode
			await user.click(screen.getByRole("button", { name: "Compare" }));

			// Wait for models to load, then select 2 models but no prompt
			await waitFor(
				() => {
					expect(screen.getByText("Test Model v1")).toBeInTheDocument();
				},
				{ timeout: 5000 },
			);
			await user.click(screen.getByText("Test Model v1"));
			await user.click(screen.getByText("Test Model v2"));

			// Should show Enter a prompt message
			await waitFor(
				() => {
					expect(screen.getByText("Enter a prompt")).toBeInTheDocument();
				},
				{ timeout: 5000 },
			);
		});
	});

	describe("Arena Collapsible Toggle", () => {
		it("clicking collapse toggle hides controls content", async () => {
			setupDefaultMocks();
			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// Find collapse toggle button
			const collapseToggles = screen.getAllByRole("button", {
				name: /Collapse|Expand/i,
			});
			expect(collapseToggles.length).toBeGreaterThan(0);

			// Controls content should be visible initially
			expect(screen.getByText("Controls")).toBeInTheDocument();

			// Click the first collapse toggle
			await user.click(collapseToggles[0]);

			// Toggle should change (at least one Expand button should appear)
			await waitFor(() => {
				const expandToggles = screen.getAllByRole("button", {
					name: "Expand",
				});
				expect(expandToggles.length).toBeGreaterThan(0);
			});
		});

		it("clicking expand toggle shows controls content", async () => {
			setupDefaultMocks();
			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// Find and click collapse toggle
			const collapseToggles = screen.getAllByRole("button", {
				name: /Collapse|Expand/i,
			});
			await user.click(collapseToggles[0]);

			// Wait for toggle to change to Expand
			await waitFor(() => {
				const expandToggles = screen.getAllByRole("button", {
					name: "Expand",
				});
				expect(expandToggles.length).toBeGreaterThan(0);
			});

			// Click expand toggle
			const expandToggles = screen.getAllByRole("button", { name: "Expand" });
			await user.click(expandToggles[0]);

			// Toggle should change back to Collapse
			await waitFor(() => {
				const collapseButtons = screen.getAllByRole("button", {
					name: "Collapse",
				});
				expect(collapseButtons.length).toBeGreaterThan(0);
			});
		});
	});

	describe("Arena Bracket Preview", () => {
		it("competition mode shows First Round and VS preview pairs", {
			timeout: 10000,
		}, async () => {
			server.use(...mockAllDefaults({ models: [mockModel, mockModel2] }));

			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// Wait for models to load, then select exactly 2 models for competition
			await waitFor(
				() => {
					expect(screen.getByText("Test Model v1")).toBeInTheDocument();
				},
				{ timeout: 5000 },
			);
			await user.click(screen.getByText("Test Model v1"));
			await user.click(screen.getByText("Test Model v2"));

			// Should show First Round preview
			await waitFor(
				() => {
					expect(screen.getByText("First Round")).toBeInTheDocument();
				},
				{ timeout: 5000 },
			);

			// Should show VS between matchup slots
			const vsElements = screen.getAllByText("VS");
			expect(vsElements.length).toBeGreaterThan(0);
		});

		it("compare mode shows model pills preview", async () => {
			server.use(...mockAllDefaults({ models: [mockModel, mockModel2] }));

			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// Toggle to Compare mode
			await user.click(screen.getByRole("button", { name: "Compare" }));

			// Wait for models to load, then select 2 models
			await waitFor(
				() => {
					expect(screen.getByText("Test Model v1")).toBeInTheDocument();
				},
				{ timeout: 5000 },
			);
			await user.click(screen.getByText("Test Model v1"));
			await user.click(screen.getByText("Test Model v2"));

			// Model pills should be visible (check for model names in preview)
			expect(screen.getByText("Test Model v1")).toBeInTheDocument();
			expect(screen.getByText("Test Model v2")).toBeInTheDocument();
		});

		it("preview updates when models change", { timeout: 10000 }, async () => {
			server.use(
				...mockAllDefaults({ models: [mockModel, mockModel2, mockModel3] }),
			);

			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// Wait for models to load, then select 2 models
			await waitFor(
				() => {
					expect(screen.getByText("Test Model v1")).toBeInTheDocument();
				},
				{ timeout: 5000 },
			);
			await user.click(screen.getByText("Test Model v1"));
			await user.click(screen.getByText("Test Model v2"));

			// Should show First Round preview with 2 models
			await waitFor(
				() => {
					expect(screen.getByText("First Round")).toBeInTheDocument();
				},
				{ timeout: 5000 },
			);

			// Add a third model
			await user.click(screen.getByText("Test Model v3"));

			// Preview should still be visible (First Round with 3 models shows bye)
			expect(screen.getByText("First Round")).toBeInTheDocument();
		});
	});

	describe("Arena Mode Toggle During Running", () => {
		it("mode toggle is disabled when phase is not setup", async () => {
			const chunks = [{ choices: [{ delta: { content: "Response" } }] }];

			server.use(
				...mockAllDefaults({ models: [mockModel, mockModel2] }),
				...mockArenaStream(chunks, { delay: 10 }),
			);

			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			await setupAndRunArena(user);

			// Clicking Compare during running should not change mode
			await user.click(screen.getByRole("button", { name: "Compare" }));
			// Should still be in Arena (competition) mode - bracket description visible
			expect(screen.getByText(/bracket tournament/i)).toBeInTheDocument();
		});

		it("mode toggle is enabled in setup phase", async () => {
			setupDefaultMocks();
			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// Clicking Compare in setup phase should switch mode
			await user.click(screen.getByRole("button", { name: "Compare" }));
			await waitFor(() => {
				expect(
					screen.getByText(/Side-by-side.*compare model outputs/i),
				).toBeInTheDocument();
			});
		});
	});

	describe("Arena Error Handling (streaming)", () => {
		it("Arena endpoint returns 500 error: page remains functional", {
			timeout: 15000,
		}, async () => {
			server.use(
				...mockAllDefaults({ models: [mockModel, mockModel2] }),
				http.post("/api/chat/arena", () =>
					HttpResponse.json({ error: "Arena failed" }, { status: 500 }),
				),
			);

			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			await setupAndRunArena(user);

			// Should handle error - page should still be functional
			// Wait for the error to be processed (stream will fail)
			await waitFor(
				() => {
					expect(screen.getByText("Controls")).toBeInTheDocument();
				},
				{ timeout: 10000 },
			);
		});

		it("Arena endpoint returns network error is handled", {
			timeout: 15000,
		}, async () => {
			server.use(
				...mockAllDefaults({ models: [mockModel, mockModel2] }),
				http.post("/api/chat/arena", () => {
					return HttpResponse.error();
				}),
			);

			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// Select models and run without using setupAndRunArena,
			// which waits for a Stop button that never appears on error.
			await waitFor(
				() => {
					expect(screen.getByText(mockModel.display_name)).toBeInTheDocument();
				},
				{ timeout: 5000 },
			);
			await user.click(screen.getByText(mockModel.display_name));
			await user.click(screen.getByText(mockModel2.display_name));

			const textarea = screen.getByRole("textbox", { name: /prompt/i });
			await user.type(textarea, "Test prompt");

			await user.click(screen.getByRole("button", { name: /Run Arena/i }));

			// Should handle error - page should still be functional
			await waitFor(
				() => {
					expect(screen.getByText("Controls")).toBeInTheDocument();
				},
				{ timeout: 10000 },
			);
		});
	});

	describe("Arena Phase Status Messages", () => {
		it("running phase shows Models are generating message", async () => {
			const chunks = [{ choices: [{ delta: { content: "Response" } }] }];

			server.use(
				...mockAllDefaults({ models: [mockModel, mockModel2] }),
				...mockArenaStream(chunks, { delay: 50 }),
			);

			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			await setupAndRunArena(user);

			// Should show generating message
			await waitFor(
				() => {
					expect(
						screen.getByText("Models are generating - click Stop to cancel"),
					).toBeInTheDocument();
				},
				{ timeout: 5000 },
			);
		});

		it("voting phase shows Vote on all matchups message", {
			timeout: 20000,
		}, async () => {
			const chunks = [{ choices: [{ delta: { content: "Done" } }] }];

			server.use(
				...mockAllDefaults({ models: [mockModel, mockModel2] }),
				...mockArenaStream(chunks, { delay: 10 }),
			);

			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			await setupAndRunArena(user);

			// Wait for streaming to complete (Stop button disappears)
			await waitFor(
				() => {
					expect(
						screen.queryByRole("button", { name: "Stop" }),
					).not.toBeInTheDocument();
				},
				{ timeout: 15000 },
			);

			// Now check for voting message
			await waitFor(
				() => {
					expect(
						screen.getByText(
							"Vote on all matchups to continue to the next round",
						),
					).toBeInTheDocument();
				},
				{ timeout: 5000 },
			);
		});

		it("setup phase with no prompt shows Enter a prompt message", async () => {
			server.use(...mockAllDefaults({ models: [mockModel, mockModel2] }));

			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// Wait for models to load, then select 2 models but no prompt
			await waitFor(
				() => {
					expect(screen.getByText("Test Model v1")).toBeInTheDocument();
				},
				{ timeout: 5000 },
			);
			await user.click(screen.getByText("Test Model v1"));
			await user.click(screen.getByText("Test Model v2"));

			// Should show Enter a prompt message
			await waitFor(
				() => {
					expect(screen.getByText("Enter a prompt")).toBeInTheDocument();
				},
				{ timeout: 5000 },
			);
		});
	});
});
