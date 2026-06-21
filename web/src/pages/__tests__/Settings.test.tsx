import { screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { Settings } from "../Settings";

describe("Settings", () => {
	beforeEach(() => {
		server.resetHandlers();
	});

	describe("Loading State", () => {
		it("renders loading spinner initially", () => {
			server.use(
				http.get("/api/settings", () => {
					return new Promise((resolve) => {
						setTimeout(() => {
							resolve(HttpResponse.json({}));
						}, 100);
					});
				}),
			);

			renderWithProviders(<Settings />);
			expect(screen.getByTestId("spinner")).toBeInTheDocument();
		});
	});

	describe("Rendering", () => {
		it("renders page header with correct title and icon", async () => {
			server.use(
				http.get("/api/settings", () => {
					return HttpResponse.json({});
				}),
			);

			renderWithProviders(<Settings />);

			await waitFor(() => {
				expect(screen.getByText("Settings")).toBeInTheDocument();
			});
			expect(
				screen.getByText("Configure your Model Hotel instance"),
			).toBeInTheDocument();
		});

		it("renders all settings sections", async () => {
			server.use(
				http.get("/api/settings", () => {
					return HttpResponse.json({});
				}),
			);

			renderWithProviders(<Settings />);

			await waitFor(() => {
				expect(screen.getByText("Settings")).toBeInTheDocument();
			});

			// All section headers should be present
			expect(screen.getByText("Model Discovery")).toBeInTheDocument();
			expect(screen.getByText("Appearance")).toBeInTheDocument();
			expect(screen.getByText("Data Storage and Logging")).toBeInTheDocument();
			expect(screen.getByText("Database Backup")).toBeInTheDocument();
			expect(screen.getByText("Rate Limiting")).toBeInTheDocument();
			expect(
				screen.getByText("Circuit Breaker & Failover"),
			).toBeInTheDocument();
			expect(screen.getByText("Proxy")).toBeInTheDocument();
		});

		it("renders section icons", async () => {
			server.use(
				http.get("/api/settings", () => {
					return HttpResponse.json({});
				}),
			);

			renderWithProviders(<Settings />);

			await waitFor(() => {
				expect(screen.getByText("Settings")).toBeInTheDocument();
			});

			// Each section should have an icon (svg element)
			const icons = document.querySelectorAll("svg");
			// Should have multiple icons (page header + each section)
			expect(icons.length).toBeGreaterThan(5);
		});

		const sections = [
			"Model Discovery",
			"Appearance",
			"Data Storage and Logging",
			"Database Backup",
			"Rate Limiting",
			"Circuit Breaker & Failover",
			"Proxy",
		];

		it.each(sections)("renders %s section", async (sectionTitle) => {
			server.use(
				http.get("/api/settings", () => {
					return HttpResponse.json({});
				}),
			);
			renderWithProviders(<Settings />);
			await waitFor(() => {
				expect(screen.getByText(sectionTitle)).toBeInTheDocument();
			});
		});
	});

	describe("Section Collapsing", () => {
		it("can collapse and expand sections", async () => {
			server.use(
				http.get("/api/settings", () => {
					return HttpResponse.json({});
				}),
			);

			const { user } = renderWithProviders(<Settings />);

			await waitFor(() => {
				expect(screen.getByText("Settings")).toBeInTheDocument();
			});

			// Find a toggle button (first CollapsibleToggle button)
			const toggleButtons = screen.getAllByRole("button");
			expect(toggleButtons.length).toBeGreaterThan(0);
			const toggleButton = toggleButtons[0];

			// Click to collapse
			await user.click(toggleButton);

			// Click to expand again
			await user.click(toggleButton);
		});
	});

	describe("API Error Handling", () => {
		it("handles settings API error gracefully", async () => {
			server.use(
				http.get("/api/settings", () => {
					return HttpResponse.json(
						{ error: "Failed to fetch" },
						{ status: 500 },
					);
				}),
			);

			renderWithProviders(<Settings />);

			// React Query retries on error by default, so component may stay in loading state
			// This test verifies the component doesn't crash on error
			await waitFor(
				() => {
					// Either spinner is shown (retrying) or page rendered (gave up)
					const hasSpinner = screen.queryByTestId("spinner");
					const hasSettingsTitle = screen.queryByText("Settings");
					expect(hasSpinner || hasSettingsTitle).toBeTruthy();
				},
				{ timeout: 3000 },
			);
		});
	});

	describe("Reset flows", () => {
		it("reset-all requires typing RESET, then calls the reset API", async () => {
			let resetCalled = false;
			server.use(
				http.get("/api/settings", () => HttpResponse.json({})),
				http.delete("/api/settings", () => {
					resetCalled = true;
					return HttpResponse.json({});
				}),
			);
			const { user } = renderWithProviders(<Settings />);
			await screen.findByText("Settings");

			await user.click(
				screen.getByRole("button", {
					name: "Reset all settings to their defaults",
				}),
			);

			// The confirm button is gated on the exact "RESET" confirmation text.
			const confirm = screen.getByRole("button", { name: "Reset to Defaults" });
			expect(confirm).toBeDisabled();
			await user.type(
				screen.getByPlaceholderText("Type RESET to confirm"),
				"RESET",
			);
			expect(confirm).toBeEnabled();

			await user.click(confirm);
			await waitFor(() => expect(resetCalled).toBe(true));
		});

		it("reset-all cancel closes the modal without calling the API", async () => {
			let resetCalled = false;
			server.use(
				http.get("/api/settings", () => HttpResponse.json({})),
				http.delete("/api/settings", () => {
					resetCalled = true;
					return HttpResponse.json({});
				}),
			);
			const { user } = renderWithProviders(<Settings />);
			await screen.findByText("Settings");

			await user.click(
				screen.getByRole("button", {
					name: "Reset all settings to their defaults",
				}),
			);
			expect(screen.getByText("Reset All Settings")).toBeInTheDocument();
			await user.click(screen.getByRole("button", { name: "Cancel" }));

			await waitFor(() =>
				expect(
					screen.queryByText("Reset All Settings"),
				).not.toBeInTheDocument(),
			);
			expect(resetCalled).toBe(false);
		});

		it("section reset confirms and calls the reset API with that section's keys", async () => {
			let capturedKeys: string[] | undefined;
			server.use(
				http.get("/api/settings", () => HttpResponse.json({})),
				http.delete("/api/settings", async ({ request }) => {
					capturedKeys = ((await request.json()) as { keys: string[] }).keys;
					return HttpResponse.json({});
				}),
			);
			const { user } = renderWithProviders(<Settings />);
			await screen.findByText("Settings");

			// Open the first section's reset confirmation, then confirm it.
			const sectionResetButtons = screen.getAllByRole("button", {
				name: "Reset all settings in this section",
			});
			await user.click(sectionResetButtons[0]);
			await user.click(
				screen.getByRole("button", { name: "Reset to Defaults" }),
			);

			await waitFor(() => expect(capturedKeys).toBeDefined());
			expect(capturedKeys?.length).toBeGreaterThan(0);
		});
	});
});
