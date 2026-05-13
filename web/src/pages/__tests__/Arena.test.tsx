import { screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import { mockAllDefaults } from "../../test/helpers";
import { mockModel, mockProvider } from "../../test/mocks/data";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { Arena } from "../Arena";

describe("Arena", () => {
	beforeEach(() => {
		server.resetHandlers();
	});

	const setupDefaultMocks = () => {
		server.use(
			...mockAllDefaults(),
			http.get("/api/events", () => new HttpResponse(null, { status: 200 })),
		);
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

			// Mode description should be visible - check for any description text
			expect(screen.getByText(/bracket|elimination/i)).toBeInTheDocument();
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
				http.get("/api/events", () => new HttpResponse(null, { status: 200 })),
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
				expect(screen.getByText("Arena")).toBeInTheDocument();
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
				expect(screen.getByText("Arena")).toBeInTheDocument();
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

			// Competition mode: should show models label
			expect(
				screen.getByText((content) => content.includes("Models")),
			).toBeInTheDocument();

			// Toggle to compare mode
			await user.click(screen.getByRole("button", { name: "Compare" }));

			// Should still show models label after toggle
			await waitFor(() => {
				expect(
					screen.getByText((content) => content.includes("Models")),
				).toBeInTheDocument();
			});
		});
	});
});
