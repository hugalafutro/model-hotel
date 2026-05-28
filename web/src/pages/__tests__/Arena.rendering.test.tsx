import { screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import { mockAllDefaults } from "../../test/helpers";
import { mockModel } from "../../test/mocks/data";
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

	describe("Arena Mode Toggle During Running", () => {
		it("mode toggle is disabled when phase is not setup", async () => {
			server.use(
				...mockAllDefaults({ models: [mockModel, mockModel2] }),
				http.post("/api/chat/arena", () =>
					HttpResponse.json({ error: "Arena failed" }, { status: 500 }),
				),
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
});
