import { screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import { mockAllDefaults, mockArenaStream } from "../../test/helpers";
import { mockModel } from "../../test/mocks/data";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { Arena } from "../Arena";
import {
	mockModel2,
	mockModel3,
	setupAndRunArena,
	waitForArenaLoad,
} from "./arena-test-helpers";

describe("Arena", () => {
	beforeEach(() => {
		server.resetHandlers();
		localStorage.removeItem("sidebarArenaSubMode");
	});

	describe("Compare Mode Streaming Flow", () => {
		it("streams responses for selected models in compare mode", {
			timeout: 8000,
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
						screen.queryByRole("button", { name: "Stop All" }),
					).not.toBeInTheDocument();
				},
				{ timeout: 2000 },
			);
		});

		it("shows Responses header in compare mode after streaming", {
			timeout: 8000,
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
						screen.queryByRole("button", { name: "Stop All" }),
					).not.toBeInTheDocument();
				},
				{ timeout: 2000 },
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
						screen.getByText(
							"Models are generating - click Stop All to cancel",
						),
					).toBeInTheDocument();
				},
				{ timeout: 2000 },
			);
		});

		it("phase transitions to finished after streaming completes in compare mode", {
			timeout: 8000,
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
						screen.queryByRole("button", { name: "Stop All" }),
					).not.toBeInTheDocument();
				},
				{ timeout: 2000 },
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
			timeout: 8000,
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
						screen.queryByRole("button", { name: "Stop All" }),
					).not.toBeInTheDocument();
				},
				{ timeout: 2000 },
			);
		});

		it("shows round label and VS between matchup slots", {
			timeout: 8000,
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
						screen.queryByRole("button", { name: "Stop All" }),
					).not.toBeInTheDocument();
				},
				{ timeout: 2000 },
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
			timeout: 8000,
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
						screen.queryByRole("button", { name: "Stop All" }),
					).not.toBeInTheDocument();
				},
				{ timeout: 2000 },
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
				{ timeout: 2000 },
			);
		});
	});

	describe("Arena Disabled Reasons", () => {
		it("competition mode with no models shows select 2 4 or 8 models", {
			timeout: 2000,
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
				{ timeout: 2000 },
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
				{ timeout: 2000 },
			);
			await user.click(screen.getByText("Test Model v1"));

			// Should show pick at least 1 more model
			await waitFor(
				() => {
					expect(
						screen.getByText("Pick at least 1 more model"),
					).toBeInTheDocument();
				},
				{ timeout: 2000 },
			);
		});

		it("competition mode with 3 models shows pick 1 more or remove to get 4", {
			timeout: 2000,
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
				{ timeout: 2000 },
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
				{ timeout: 2000 },
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
				{ timeout: 2000 },
			);
			await user.click(screen.getByText("Test Model v1"));

			// Should show pick at least 1 more model
			await waitFor(
				() => {
					expect(
						screen.getByText("Pick at least 1 more model"),
					).toBeInTheDocument();
				},
				{ timeout: 2000 },
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
				{ timeout: 2000 },
			);
			await user.click(screen.getByText("Test Model v1"));
			await user.click(screen.getByText("Test Model v2"));

			// Should show Enter a prompt message
			await waitFor(
				() => {
					expect(screen.getByText("Enter a prompt")).toBeInTheDocument();
				},
				{ timeout: 2000 },
			);
		});
	});

	describe("Arena Bracket Preview", () => {
		it("competition mode shows First Round and VS preview pairs", {
			timeout: 2000,
		}, async () => {
			server.use(...mockAllDefaults({ models: [mockModel, mockModel2] }));

			const { user } = renderWithProviders(<Arena />);
			await waitForArenaLoad();

			// Wait for models to load, then select exactly 2 models for competition
			await waitFor(
				() => {
					expect(screen.getByText("Test Model v1")).toBeInTheDocument();
				},
				{ timeout: 2000 },
			);
			await user.click(screen.getByText("Test Model v1"));
			await user.click(screen.getByText("Test Model v2"));

			// Should show First Round preview
			await waitFor(
				() => {
					expect(screen.getByText("First Round")).toBeInTheDocument();
				},
				{ timeout: 2000 },
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
				{ timeout: 2000 },
			);
			await user.click(screen.getByText("Test Model v1"));
			await user.click(screen.getByText("Test Model v2"));

			// Model pills should be visible (check for model names in preview)
			expect(screen.getAllByText("Test Model v1").length).toBeGreaterThan(0);
			expect(screen.getAllByText("Test Model v2").length).toBeGreaterThan(0);
		});

		it("preview updates when models change", { timeout: 2000 }, async () => {
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
				{ timeout: 2000 },
			);
			await user.click(screen.getByText("Test Model v1"));
			await user.click(screen.getByText("Test Model v2"));

			// Should show First Round preview with 2 models
			await waitFor(
				() => {
					expect(screen.getByText("First Round")).toBeInTheDocument();
				},
				{ timeout: 2000 },
			);

			// Add a third model
			await user.click(screen.getByText("Test Model v3"));

			// Preview should still be visible (First Round with 3 models shows bye)
			expect(screen.getByText("First Round")).toBeInTheDocument();
		});
	});

	describe("Arena Error Handling (streaming)", () => {
		it("Arena endpoint returns 500 error: page remains functional", {
			timeout: 8000,
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
				{ timeout: 2000 },
			);
		});

		it("Arena endpoint returns network error is handled", {
			timeout: 8000,
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
				{ timeout: 2000 },
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
				{ timeout: 2000 },
			);
		});
	});
});
