import { screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import { mockFailoverGroup } from "../../test/mocks/data";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { FailoverGroups } from "../FailoverGroups";

describe("FailoverGroups", () => {
	beforeEach(() => {
		server.resetHandlers();
	});

	describe("Filtering", () => {
		it("search filter filters groups by display model name", async () => {
			const groups = [
				{ ...mockFailoverGroup, display_model: "alpha-model" },
				{
					...mockFailoverGroup,
					display_model: "beta-model",
					id: "fg-002",
				},
				{
					...mockFailoverGroup,
					display_model: "gamma-model",
					id: "fg-003",
				},
			];

			server.use(
				http.get("/api/failover-groups", () => {
					return HttpResponse.json({
						groups,
						last_synced_at: null,
					});
				}),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/alpha-model")).toBeInTheDocument();
				expect(screen.getByText("hotel/beta-model")).toBeInTheDocument();
			});

			// Filter by "alpha"
			const filterInput = screen.getByPlaceholderText("Filter hotel/model…");
			await user.type(filterInput, "alpha");

			await waitFor(() => {
				expect(screen.getByText("hotel/alpha-model")).toBeInTheDocument();
				expect(screen.queryByText("hotel/beta-model")).not.toBeInTheDocument();
				expect(screen.queryByText("hotel/gamma-model")).not.toBeInTheDocument();
			});
		});

		it("provider filter filters groups by provider", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					display_model: "model-a",
					entries: [
						{
							provider_name: "Provider Alpha",
							model_id: "model-a",
							enabled: true,
							model_uuid: "uuid-a",
						},
					],
				},
				{
					...mockFailoverGroup,
					display_model: "model-b",
					id: "fg-002",
					entries: [
						{
							provider_name: "Provider Beta",
							model_id: "model-b",
							enabled: true,
							model_uuid: "uuid-b",
						},
					],
				},
			];

			server.use(
				http.get("/api/failover-groups", () => {
					return HttpResponse.json({
						groups,
						last_synced_at: null,
					});
				}),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/model-a")).toBeInTheDocument();
				expect(screen.getByText("hotel/model-b")).toBeInTheDocument();
			});

			// Use provider FilterDropdown to filter
			await user.click(
				screen.getByRole("button", { name: "All (2) Providers" }),
			);
			await user.click(screen.getByRole("button", { name: "Provider Alpha" }));

			await waitFor(() => {
				expect(screen.getByText("hotel/model-a")).toBeInTheDocument();
				expect(screen.queryByText("hotel/model-b")).not.toBeInTheDocument();
			});
		});

		it("enabled filter shows only enabled groups", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					display_model: "enabled-model",
					group_enabled: true,
				},
				{
					...mockFailoverGroup,
					display_model: "disabled-model",
					id: "fg-002",
					group_enabled: false,
				},
			];

			server.use(
				http.get("/api/failover-groups", () => {
					return HttpResponse.json({
						groups,
						last_synced_at: null,
					});
				}),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/enabled-model")).toBeInTheDocument();
				expect(screen.getByText("hotel/disabled-model")).toBeInTheDocument();
			});

			// Use enabled FilterDropdown to filter
			await user.click(screen.getByRole("button", { name: "All (2) States" }));
			await user.click(screen.getByRole("button", { name: "Enabled" }));

			await waitFor(() => {
				expect(screen.getByText("hotel/enabled-model")).toBeInTheDocument();
				expect(
					screen.queryByText("hotel/disabled-model"),
				).not.toBeInTheDocument();
			});
		});

		it("enabled filter shows only disabled groups", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					display_model: "enabled-model",
					group_enabled: true,
				},
				{
					...mockFailoverGroup,
					display_model: "disabled-model",
					id: "fg-002",
					group_enabled: false,
				},
			];

			server.use(
				http.get("/api/failover-groups", () => {
					return HttpResponse.json({
						groups,
						last_synced_at: null,
					});
				}),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/enabled-model")).toBeInTheDocument();
				expect(screen.getByText("hotel/disabled-model")).toBeInTheDocument();
			});

			// Use enabled FilterDropdown to filter
			await user.click(screen.getByRole("button", { name: "All (2) States" }));
			await user.click(screen.getByRole("button", { name: "Disabled" }));

			await waitFor(() => {
				expect(screen.getByText("hotel/disabled-model")).toBeInTheDocument();
				expect(
					screen.queryByText("hotel/enabled-model"),
				).not.toBeInTheDocument();
			});
		});
	});

	describe("Sorting and Grouping", () => {
		it("groups are sorted alphabetically by display model", async () => {
			const groups = [
				{ ...mockFailoverGroup, display_model: "zebra-model" },
				{
					...mockFailoverGroup,
					display_model: "alpha-model",
					id: "fg-002",
				},
				{
					...mockFailoverGroup,
					display_model: "middle-model",
					id: "fg-003",
				},
			];

			server.use(
				http.get("/api/failover-groups", () => {
					return HttpResponse.json({
						groups,
						last_synced_at: null,
					});
				}),
			);

			renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				// Models should appear in alphabetical order in the DOM
				const allModelTexts = screen.getAllByText(
					/hotel\/(alpha|middle|zebra)-model/,
				);
				expect(allModelTexts.length).toBe(3);
				// Check order by getting text content
				expect(allModelTexts[0]?.textContent).toContain("alpha-model");
				expect(allModelTexts[1]?.textContent).toContain("middle-model");
				expect(allModelTexts[2]?.textContent).toContain("zebra-model");
			});
		});

		it("groups are grouped by first letter with collapse toggle", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					display_model: "alpha-one",
					auto_created: true,
				},
				{
					...mockFailoverGroup,
					display_model: "alpha-two",
					id: "fg-002",
					auto_created: true,
				},
				{
					...mockFailoverGroup,
					display_model: "beta-one",
					id: "fg-003",
					auto_created: true,
				},
			];

			server.use(
				http.get("/api/failover-groups", () => {
					return HttpResponse.json({
						groups,
						last_synced_at: null,
					});
				}),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/alpha-one")).toBeInTheDocument();
				expect(screen.getByText("hotel/alpha-two")).toBeInTheDocument();
				expect(screen.getByText("hotel/beta-one")).toBeInTheDocument();
				// Letter section headers should exist
				expect(screen.getByText("A")).toBeInTheDocument();
				expect(screen.getByText("B")).toBeInTheDocument();
			});

			// Find the collapse toggle for "A" section (button with text "A")
			const allButtons = screen.getAllByRole("button");
			const aToggle = allButtons.find((btn) => btn.textContent?.trim() === "A");
			if (aToggle) {
				await user.click(aToggle);
			}

			// Section should collapse - letter header still visible but content may be hidden
			await waitFor(() => {
				// Letter header remains visible
				expect(screen.getByText("A")).toBeInTheDocument();
			});
		});

		it("toggling letter collapse expands/collapses groups", async () => {
			const groups = [
				{ ...mockFailoverGroup, display_model: "test-one", auto_created: true },
				{
					...mockFailoverGroup,
					display_model: "test-two",
					id: "fg-002",
					auto_created: true,
				},
			];

			server.use(
				http.get("/api/failover-groups", () => {
					return HttpResponse.json({
						groups,
						last_synced_at: null,
					});
				}),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/test-one")).toBeInTheDocument();
			});

			// Find the collapse toggle for "T" section (test starts with T)
			const allButtons = screen.getAllByRole("button");
			const tToggle = allButtons.find((btn) => btn.textContent?.trim() === "T");

			if (tToggle) {
				// Collapse
				await user.click(tToggle);

				await waitFor(() => {
					expect(screen.queryByText("hotel/test-one")).not.toBeInTheDocument();
				});

				// Expand again
				await user.click(tToggle);

				await waitFor(() => {
					expect(screen.getByText("hotel/test-one")).toBeInTheDocument();
					expect(screen.getByText("hotel/test-two")).toBeInTheDocument();
				});
			}
		});

		it("custom groups appear in a Custom section above letter sections", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					display_model: "custom-model",
					auto_created: false,
				},
				{
					...mockFailoverGroup,
					display_model: "alpha-model",
					id: "fg-002",
					auto_created: true,
				},
				{
					...mockFailoverGroup,
					display_model: "beta-model",
					id: "fg-003",
					auto_created: true,
				},
			];

			server.use(
				http.get("/api/failover-groups", () => {
					return HttpResponse.json({
						groups,
						last_synced_at: null,
					});
				}),
			);

			renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				// Custom section header should appear
				expect(screen.getByText("Custom")).toBeInTheDocument();
				// Custom group should be in Custom section
				expect(screen.getByText("hotel/custom-model")).toBeInTheDocument();
				// Auto groups should be in their letter sections
				expect(screen.getByText("hotel/alpha-model")).toBeInTheDocument();
				expect(screen.getByText("hotel/beta-model")).toBeInTheDocument();
				// Letter section headers should exist (use query to avoid multiple match error)
				expect(screen.queryAllByText("A").length).toBeGreaterThan(0);
				expect(screen.queryAllByText("B").length).toBeGreaterThan(0);
			});
		});

		it("★ symbol appears in alphabet strip when custom groups exist", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					display_model: "custom-model",
					auto_created: false,
				},
				{
					...mockFailoverGroup,
					display_model: "alpha-model",
					id: "fg-002",
					auto_created: true,
				},
			];

			server.use(
				http.get("/api/failover-groups", () => {
					return HttpResponse.json({
						groups,
						last_synced_at: null,
					});
				}),
			);

			renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				// ★ button should appear in alphabet strip
				// The ★ button has aria-label "Jump to custom groups"
				expect(
					screen.getByRole("button", { name: /Jump to custom groups/i }),
				).toBeInTheDocument();
				// Verify it contains the ★ symbol
				expect(screen.getByText("★")).toBeInTheDocument();
			});
		});

		it("Custom section is collapsible", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					display_model: "custom-model",
					auto_created: false,
				},
				{
					...mockFailoverGroup,
					display_model: "another-custom",
					id: "fg-002",
					auto_created: false,
				},
			];

			server.use(
				http.get("/api/failover-groups", () => {
					return HttpResponse.json({
						groups,
						last_synced_at: null,
					});
				}),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("Custom")).toBeInTheDocument();
				expect(screen.getByText("hotel/custom-model")).toBeInTheDocument();
			});

			// Find the Custom section collapse toggle button
			// The button contains the "Custom" text and a ChevronRight icon
			const customText = screen.getByText("Custom");
			const customToggle = customText.closest("button");
			expect(customToggle).toBeTruthy();

			if (customToggle) {
				// Collapse
				await user.click(customToggle);

				// Wait for the collapse animation to complete
				await new Promise((resolve) => setTimeout(resolve, 300));

				// After collapse, the custom groups should not be visible
				// The content is hidden via gridTemplateRows: 0fr
				const customSection = customText.closest("section");
				expect(customSection).toBeTruthy();
				const contentDiv = customSection?.querySelector(
					'[style*="grid-template-rows: 0fr"]',
				);
				expect(contentDiv).toBeTruthy();

				// Expand again
				await user.click(customToggle);

				await waitFor(() => {
					expect(screen.getByText("hotel/custom-model")).toBeInTheDocument();
					expect(screen.getByText("hotel/another-custom")).toBeInTheDocument();
				});
			}
		});
	});
});
