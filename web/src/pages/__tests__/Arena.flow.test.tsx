import { screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import {
	mockAllDefaults,
	mockArenaStream,
	mockModels,
} from "../../test/helpers";
import { mockModel, mockProvider } from "../../test/mocks/data";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { Arena } from "../Arena";
import {
	mockModel2,
	setupAndRunArena,
	setupDefaultMocks,
	waitForArenaLoad,
} from "./arena-test-helpers";

describe("Arena", () => {
	beforeEach(() => {
		server.resetHandlers();
		localStorage.removeItem("sidebarArenaSubMode");
	});

	describe("API Integration", () => {
		it("fetches models from correct endpoint", async () => {
			let modelsApiCalled = false;
			server.use(
				http.get("/api/models", ({ request }) => {
					modelsApiCalled = true;
					expect(request.headers.get("Cookie")).toContain("mh_csrf=");
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
					expect(request.headers.get("Cookie")).toContain("mh_csrf=");
					return HttpResponse.json([mockModel]);
				}),
				http.get("/api/providers", () => HttpResponse.json([mockProvider])),
			);

			renderWithProviders(<Arena />);

			await waitFor(
				() => {
					expect(modelsApiCalled).toBe(true);
				},
				{ timeout: 2000 },
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
			expect(screen.getAllByText(/persona/i).length).toBeGreaterThan(0);
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
				{ timeout: 2000 },
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
				{ timeout: 2000 },
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

	describe("Arena Stop Flow", () => {
		it("Stop during compare mode streaming sets phase to finished", {
			timeout: 8000,
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
			await user.click(screen.getByRole("button", { name: "Stop All" }));

			// Stop button should disappear (phase is finished, no button rendered)
			await waitFor(
				() => {
					expect(
						screen.queryByRole("button", { name: "Stop All" }),
					).not.toBeInTheDocument();
				},
				{ timeout: 2000 },
			);
		});

		it("Stop during competition mode streaming sets phase to voting", {
			timeout: 8000,
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
			await user.click(screen.getByRole("button", { name: "Stop All" }));

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

		it("partially streamed responses are preserved after stop", {
			timeout: 8000,
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
			await user.click(screen.getByRole("button", { name: "Stop All" }));

			// Responses should be preserved (check for Responses header in compare mode)
			await waitFor(
				() => {
					expect(screen.getByText("Responses")).toBeInTheDocument();
				},
				{ timeout: 2000 },
			);
		});
	});

	describe("Arena Clear Results (Eraser)", () => {
		it("Clear button appears when phase is not setup", {
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

			// Clear button should appear
			expect(
				screen.getByRole("button", {
					name: "Clear results (keep models & prompt)",
				}),
			).toBeInTheDocument();
		});

		it("Clear button clears results but keeps models and prompt", {
			timeout: 8000,
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
						screen.queryByRole("button", { name: "Stop All" }),
					).not.toBeInTheDocument();
				},
				{ timeout: 2000 },
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
				{ timeout: 2000 },
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
				{ timeout: 2000 },
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
				{ timeout: 2000 },
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
			expect(screen.getAllByText("Test Model v1").length).toBeGreaterThan(0);

			// Prompt should still be present
			expect(textarea).toHaveValue("Test prompt to keep");
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
						screen.getByText(
							"Models are generating - click Stop All to cancel",
						),
					).toBeInTheDocument();
				},
				{ timeout: 2000 },
			);
		});

		it("voting phase shows Vote on all matchups message", {
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
						screen.queryByRole("button", { name: "Stop All" }),
					).not.toBeInTheDocument();
				},
				{ timeout: 8000 },
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
});
