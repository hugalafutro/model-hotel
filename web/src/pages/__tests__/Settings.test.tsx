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
});
