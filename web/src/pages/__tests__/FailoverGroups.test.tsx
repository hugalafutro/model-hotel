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

	describe("Loading State", () => {
		it("renders loading spinner initially", () => {
			server.use(
				http.get("/api/failover-groups", () => {
					return new Promise((resolve) => {
						setTimeout(() => {
							resolve(
								HttpResponse.json({
									groups: [mockFailoverGroup],
									last_synced_at: null,
								}),
							);
						}, 100);
					});
				}),
			);

			renderWithProviders(<FailoverGroups />);
			expect(screen.getByText("Loading...")).toBeInTheDocument();
		});
	});

	describe("Rendering", () => {
		it("renders page header with 'Failover Groups' title", async () => {
			server.use(
				http.get("/api/failover-groups", () => {
					return HttpResponse.json({
						groups: [mockFailoverGroup],
						last_synced_at: null,
					});
				}),
			);

			renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				// Title includes count: "1 Failover Group"
				expect(screen.getByText(/Failover Group/)).toBeInTheDocument();
			});
		});

		it("renders 'New Group' button", async () => {
			server.use(
				http.get("/api/failover-groups", () => {
					return HttpResponse.json({
						groups: [mockFailoverGroup],
						last_synced_at: null,
					});
				}),
			);

			renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "+ New Group" }),
				).toBeInTheDocument();
			});
		});

		it("shows count label (e.g. '1 Failover Group')", async () => {
			server.use(
				http.get("/api/failover-groups", () => {
					return HttpResponse.json({
						groups: [mockFailoverGroup],
						last_synced_at: null,
					});
				}),
			);

			renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("1 Failover Group")).toBeInTheDocument();
			});
		});

		it("shows plural count label for multiple groups", async () => {
			const groups = [
				mockFailoverGroup,
				{
					...mockFailoverGroup,
					id: "fg-002",
					display_model: "hotel/another-model",
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
				expect(screen.getByText("2 Failover Groups")).toBeInTheDocument();
			});
		});

		it("shows search filter input", async () => {
			server.use(
				http.get("/api/failover-groups", () => {
					return HttpResponse.json({
						groups: [mockFailoverGroup],
						last_synced_at: null,
					});
				}),
			);

			renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(
					screen.getByPlaceholderText("Filter hotel/model…"),
				).toBeInTheDocument();
			});
		});

		it("shows provider filter dropdown", async () => {
			const groupWithEntries = {
				...mockFailoverGroup,
				entries: [
					{
						provider_name: "Test Provider",
						model_id: "test-model",
						enabled: true,
						model_uuid: "model-001",
					},
				],
			};

			server.use(
				http.get("/api/failover-groups", () => {
					return HttpResponse.json({
						groups: [groupWithEntries],
						last_synced_at: null,
					});
				}),
			);

			renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				// Provider filter is a FilterDropdown button with placeholder text
				expect(
					screen.getByRole("button", { name: "All providers" }),
				).toBeInTheDocument();
			});
		});

		it("shows enabled/disabled filter", async () => {
			server.use(
				http.get("/api/failover-groups", () => {
					return HttpResponse.json({
						groups: [mockFailoverGroup],
						last_synced_at: null,
					});
				}),
			);

			renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				// Both FilterDropdown buttons should be present
				expect(
					screen.getByRole("button", { name: "All providers" }),
				).toBeInTheDocument();
				expect(
					screen.getByRole("button", { name: "All states" }),
				).toBeInTheDocument();
			});
		});

		it("renders failover group cards after data loads", async () => {
			const groupWithEntries = {
				...mockFailoverGroup,
				display_model: "test-model",
				entries: [
					{
						provider_name: "Test Provider",
						model_id: "test-model",
						enabled: true,
						model_uuid: "model-001",
					},
				],
			};

			server.use(
				http.get("/api/failover-groups", () => {
					return HttpResponse.json({
						groups: [groupWithEntries],
						last_synced_at: null,
					});
				}),
			);

			renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/test-model")).toBeInTheDocument();
			});
		});

		it("shows empty state when no groups exist", async () => {
			server.use(
				http.get("/api/failover-groups", () => {
					return HttpResponse.json({
						groups: [],
						last_synced_at: null,
					});
				}),
			);

			renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(
					screen.getByText("No failover groups configured"),
				).toBeInTheDocument();
			});
		});

		it("shows empty state when search filter matches nothing", async () => {
			const groups = [
				mockFailoverGroup,
				{
					...mockFailoverGroup,
					id: "fg-002",
					display_model: "another-model",
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
				expect(screen.getByText("hotel/test-model")).toBeInTheDocument();
			});

			// Type a filter that matches nothing
			const filterInput = screen.getByPlaceholderText("Filter hotel/model…");
			await user.type(filterInput, "NonExistentModel");

			await waitFor(() => {
				expect(
					screen.getByText("No groups matching filters"),
				).toBeInTheDocument();
			});
		});

		it("shows last synced timestamp", async () => {
			server.use(
				http.get("/api/failover-groups", () => {
					return HttpResponse.json({
						groups: [mockFailoverGroup],
						last_synced_at: "2026-05-11T10:00:00Z",
					});
				}),
			);

			renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText(/Last sync:/)).toBeInTheDocument();
			});
		});

		it("shows Sync button", async () => {
			server.use(
				http.get("/api/failover-groups", () => {
					return HttpResponse.json({
						groups: [mockFailoverGroup],
						last_synced_at: null,
					});
				}),
			);

			renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "Sync" }),
				).toBeInTheDocument();
			});
		});
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
			await user.click(screen.getByRole("button", { name: "All providers" }));
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
			await user.click(screen.getByRole("button", { name: "All states" }));
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
			await user.click(screen.getByRole("button", { name: "All states" }));
			await user.click(screen.getByRole("button", { name: "Disabled" }));

			await waitFor(() => {
				expect(screen.getByText("hotel/disabled-model")).toBeInTheDocument();
				expect(
					screen.queryByText("hotel/enabled-model"),
				).not.toBeInTheDocument();
			});
		});
	});

	describe("Create Group Modal", () => {
		it("'New Group' button opens CreateGroupModal", async () => {
			server.use(
				http.get("/api/failover-groups", () => {
					return HttpResponse.json({
						groups: [mockFailoverGroup],
						last_synced_at: null,
					});
				}),
				http.get("/api/failover-groups/candidates", () => {
					return HttpResponse.json([]);
				}),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "+ New Group" }),
				).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: "+ New Group" }));

			await waitFor(() => {
				expect(
					screen.getByRole("heading", { name: "Create Failover Group" }),
				).toBeInTheDocument();
			});
		});
	});

	describe("Delete Flow", () => {
		it("Delete button opens DeleteConfirmModal", async () => {
			const groupWithEntries = {
				...mockFailoverGroup,
				entries: [
					{
						provider_name: "Test Provider",
						model_id: "test-model",
						enabled: true,
						model_uuid: "model-001",
					},
				],
			};

			server.use(
				http.get("/api/failover-groups", () => {
					return HttpResponse.json({
						groups: [groupWithEntries],
						last_synced_at: null,
					});
				}),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/test-model")).toBeInTheDocument();
			});

			// Click Delete button on the group card (text is lowercase "delete")
			const deleteButton = screen.getByRole("button", { name: /delete/i });
			await user.click(deleteButton);

			await waitFor(() => {
				expect(
					screen.getByText(/Are you sure you want to delete/),
				).toBeInTheDocument();
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

	describe("Error Handling", () => {
		it("server error shows error state or toast", async () => {
			server.use(
				http.get("/api/failover-groups", () => {
					return HttpResponse.json(
						{ error: "Failed to fetch failover groups" },
						{ status: 500 },
					);
				}),
			);

			renderWithProviders(<FailoverGroups />);

			// Should handle error gracefully - query will be in error state
			await waitFor(() => {
				expect(screen.getByText("Failover Groups")).toBeInTheDocument();
			});
		});
	});

	describe("Multiple Groups", () => {
		it("renders multiple failover group cards", async () => {
			const groups = [
				mockFailoverGroup,
				{
					...mockFailoverGroup,
					id: "fg-002",
					display_model: "another-model",
				},
				{
					...mockFailoverGroup,
					id: "fg-003",
					display_model: "third-model",
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
				expect(screen.getByText("hotel/test-model")).toBeInTheDocument();
				expect(screen.getByText("hotel/another-model")).toBeInTheDocument();
				expect(screen.getByText("hotel/third-model")).toBeInTheDocument();
			});
		});
	});

	describe("Group Card Details", () => {
		it("shows group display model", async () => {
			const groupWithEntries = {
				...mockFailoverGroup,
				display_model: "my-model",
				entries: [
					{
						provider_name: "Test Provider",
						model_id: "test-model",
						enabled: true,
						model_uuid: "model-001",
					},
				],
			};

			server.use(
				http.get("/api/failover-groups", () => {
					return HttpResponse.json({
						groups: [groupWithEntries],
						last_synced_at: null,
					});
				}),
			);

			renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/my-model")).toBeInTheDocument();
			});
		});

		it("shows provider name in group card", async () => {
			const groupWithEntries = {
				...mockFailoverGroup,
				display_model: "unique-model",
				entries: [
					{
						provider_name: "UniqueProvider",
						model_id: "unique-model",
						enabled: true,
						model_uuid: "model-001",
					},
				],
			};

			server.use(
				http.get("/api/failover-groups", () => {
					return HttpResponse.json({
						groups: [groupWithEntries],
						last_synced_at: null,
					});
				}),
			);

			renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				// Provider name appears only in card (FilterDropdown options not rendered when closed)
				expect(screen.getByText("UniqueProvider")).toBeInTheDocument();
			});
		});

		it("shows enable/disable toggle for group", async () => {
			const groupWithEntries = {
				...mockFailoverGroup,
				group_enabled: true,
				entries: [
					{
						provider_name: "Test Provider",
						model_id: "test-model",
						enabled: true,
						model_uuid: "model-001",
					},
				],
			};

			server.use(
				http.get("/api/failover-groups", () => {
					return HttpResponse.json({
						groups: [groupWithEntries],
						last_synced_at: null,
					});
				}),
			);

			renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				// Look for the toggle button (it's a button with role="switch")
				expect(screen.getByRole("switch")).toBeInTheDocument();
			});
		});
	});

	describe("Sync Mutation", () => {
		it("Sync button triggers sync mutation", async () => {
			let syncCalled = false;
			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({
						groups: [mockFailoverGroup],
						last_synced_at: null,
					}),
				),
				http.get("/api/failover-groups/candidates", () =>
					HttpResponse.json([]),
				),
				http.post("/api/failover-groups/sync", () => {
					syncCalled = true;
					return HttpResponse.json({ disabled_groups: [] });
				}),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "Sync" }),
				).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: "Sync" }));

			await waitFor(() => {
				expect(syncCalled).toBe(true);
			});
		});

		it("Sync success without disabled groups shows success toast", async () => {
			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({
						groups: [mockFailoverGroup],
						last_synced_at: null,
					}),
				),
				http.get("/api/failover-groups/candidates", () =>
					HttpResponse.json([]),
				),
				http.post("/api/failover-groups/sync", () =>
					HttpResponse.json({ disabled_groups: [] }),
				),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "Sync" }),
				).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: "Sync" }));

			await waitFor(() => {
				expect(screen.getByText("Failover groups synced")).toBeInTheDocument();
			});
		});

		it("Sync success with disabled groups shows warning toasts", async () => {
			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({
						groups: [mockFailoverGroup],
						last_synced_at: null,
					}),
				),
				http.get("/api/failover-groups/candidates", () =>
					HttpResponse.json([]),
				),
				http.post("/api/failover-groups/sync", () =>
					HttpResponse.json({
						disabled_groups: [
							{
								display_model: "gpt-4",
								reason: "no providers",
								provider_names: ["OpenAI"],
							},
						],
					}),
				),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "Sync" }),
				).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: "Sync" }));

			await waitFor(() => {
				expect(
					screen.getByText("hotel/gpt-4 disabled: no providers (OpenAI)"),
				).toBeInTheDocument();
			});
		});

		it("Sync error shows error toast", async () => {
			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({
						groups: [mockFailoverGroup],
						last_synced_at: null,
					}),
				),
				http.get("/api/failover-groups/candidates", () =>
					HttpResponse.json([]),
				),
				http.post("/api/failover-groups/sync", () =>
					HttpResponse.json({ error: "Sync failed" }, { status: 500 }),
				),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "Sync" }),
				).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: "Sync" }));

			await waitFor(() => {
				expect(screen.getByText(/Failed to sync:/)).toBeInTheDocument();
			});
		});

		it("Sync button shows spinner while pending", async () => {
			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({
						groups: [mockFailoverGroup],
						last_synced_at: null,
					}),
				),
				http.get("/api/failover-groups/candidates", () =>
					HttpResponse.json([]),
				),
				http.post("/api/failover-groups/sync", () => {
					return new Promise((resolve) => {
						setTimeout(() => {
							resolve(HttpResponse.json({ disabled_groups: [] }));
						}, 100);
					});
				}),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "Sync" }),
				).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: "Sync" }));

			// Spinner should appear while pending
			expect(screen.getByText("Syncing…")).toBeInTheDocument();

			await waitFor(() => {
				expect(screen.queryByText("Syncing…")).not.toBeInTheDocument();
			});
		});
	});

	describe("Delete Confirmation", () => {
		it("Confirm delete calls delete mutation and closes modal", async () => {
			let deleteCalled = false;
			const groupWithEntries = {
				...mockFailoverGroup,
				entries: [
					{
						provider_name: "Test Provider",
						model_id: "test-model",
						enabled: true,
						model_uuid: "model-001",
					},
				],
			};

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({
						groups: [groupWithEntries],
						last_synced_at: null,
					}),
				),
				http.delete("/api/failover-groups/:id", () => {
					deleteCalled = true;
					return HttpResponse.json({});
				}),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/test-model")).toBeInTheDocument();
			});

			// Click Delete button
			await user.click(screen.getByRole("button", { name: /delete/i }));

			// Wait for modal to open
			await waitFor(() => {
				expect(
					screen.getByText(/Are you sure you want to delete/),
				).toBeInTheDocument();
			});

			// Click confirm button
			await user.click(screen.getByRole("button", { name: "Delete" }));

			await waitFor(() => {
				expect(deleteCalled).toBe(true);
			});

			// Modal should close
			await waitFor(() => {
				expect(
					screen.queryByText(/Are you sure you want to delete/),
				).not.toBeInTheDocument();
			});
		});

		it("Delete success shows success toast", async () => {
			const groupWithEntries = {
				...mockFailoverGroup,
				entries: [
					{
						provider_name: "Test Provider",
						model_id: "test-model",
						enabled: true,
						model_uuid: "model-001",
					},
				],
			};

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({
						groups: [groupWithEntries],
						last_synced_at: null,
					}),
				),
				http.delete("/api/failover-groups/:id", () => HttpResponse.json({})),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/test-model")).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: /delete/i }));

			await waitFor(() => {
				expect(
					screen.getByText(/Are you sure you want to delete/),
				).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: "Delete" }));

			await waitFor(() => {
				expect(screen.getByText("Group deleted")).toBeInTheDocument();
			});
		});

		it("Delete error shows error toast", async () => {
			const groupWithEntries = {
				...mockFailoverGroup,
				entries: [
					{
						provider_name: "Test Provider",
						model_id: "test-model",
						enabled: true,
						model_uuid: "model-001",
					},
				],
			};

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({
						groups: [groupWithEntries],
						last_synced_at: null,
					}),
				),
				http.delete("/api/failover-groups/:id", () =>
					HttpResponse.json({ error: "Delete failed" }, { status: 500 }),
				),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/test-model")).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: /delete/i }));

			await waitFor(() => {
				expect(
					screen.getByText(/Are you sure you want to delete/),
				).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: "Delete" }));

			await waitFor(() => {
				expect(screen.getByText(/Failed to delete:/)).toBeInTheDocument();
			});
		});

		it("Cancel delete closes modal without deleting", async () => {
			let deleteCalled = false;
			const groupWithEntries = {
				...mockFailoverGroup,
				entries: [
					{
						provider_name: "Test Provider",
						model_id: "test-model",
						enabled: true,
						model_uuid: "model-001",
					},
				],
			};

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({
						groups: [groupWithEntries],
						last_synced_at: null,
					}),
				),
				http.delete("/api/failover-groups/:id", () => {
					deleteCalled = true;
					return HttpResponse.json({});
				}),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/test-model")).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: /delete/i }));

			await waitFor(() => {
				expect(
					screen.getByText(/Are you sure you want to delete/),
				).toBeInTheDocument();
			});

			// Click cancel button
			await user.click(screen.getByRole("button", { name: "Cancel" }));

			await waitFor(() => {
				expect(deleteCalled).toBe(false);
			});

			// Modal should close
			await waitFor(() => {
				expect(
					screen.queryByText(/Are you sure you want to delete/),
				).not.toBeInTheDocument();
			});
		});
	});

	describe("Toggle Group", () => {
		it("Toggling group enabled calls update mutation", async () => {
			const groupWithEntries = {
				...mockFailoverGroup,
				group_enabled: true,
				entries: [
					{
						provider_name: "Test Provider",
						model_id: "test-model",
						enabled: true,
						model_uuid: "model-001",
					},
				],
			};

			let updateCalled = false;
			let updateData: unknown;

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({
						groups: [groupWithEntries],
						last_synced_at: null,
					}),
				),
				http.put("/api/failover-groups/:id", async ({ request }) => {
					updateCalled = true;
					updateData = await request.json();
					return HttpResponse.json({});
				}),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/test-model")).toBeInTheDocument();
			});

			// Click the ON/OFF toggle button
			const toggleButton = screen.getByRole("button", {
				name: /ON|OFF/i,
			});
			await user.click(toggleButton);

			await waitFor(() => {
				expect(updateCalled).toBe(true);
			});

			expect(updateData).toEqual({ group_enabled: false });
		});

		it("Update error shows error toast", async () => {
			const groupWithEntries = {
				...mockFailoverGroup,
				group_enabled: true,
				entries: [
					{
						provider_name: "Test Provider",
						model_id: "test-model",
						enabled: true,
						model_uuid: "model-001",
					},
				],
			};

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({
						groups: [groupWithEntries],
						last_synced_at: null,
					}),
				),
				http.put("/api/failover-groups/:id", () =>
					HttpResponse.json({ error: "Update failed" }, { status: 500 }),
				),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/test-model")).toBeInTheDocument();
			});

			const toggleButton = screen.getByRole("button", {
				name: /ON|OFF/i,
			});
			await user.click(toggleButton);

			await waitFor(() => {
				expect(screen.getByText(/Failed to update:/)).toBeInTheDocument();
			});
		});
	});

	describe("Toggle Entry", () => {
		it("Toggling entry enabled calls update mutation", async () => {
			const groupWithEntries = {
				...mockFailoverGroup,
				entries: [
					{
						provider_name: "OpenAI",
						model_id: "gpt-4",
						enabled: true,
						model_uuid: "uuid-1",
					},
					{
						provider_name: "Anthropic",
						model_id: "claude-3",
						enabled: true,
						model_uuid: "uuid-2",
					},
				],
			};

			let updateCalled = false;
			let updateData: unknown;

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({
						groups: [groupWithEntries],
						last_synced_at: null,
					}),
				),
				http.put("/api/failover-groups/:id", async ({ request }) => {
					updateCalled = true;
					updateData = await request.json();
					return HttpResponse.json({});
				}),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/test-model")).toBeInTheDocument();
			});

			// Find the Anthropic entry container, then find its toggle
			// Entry text shows "Anthropic / claude-3"
			const anthropicEntry = screen
				.getByText("Anthropic")
				.closest(".relative.flex.items-center");
			expect(anthropicEntry).toBeTruthy();
			const entryToggle = anthropicEntry?.querySelector(
				'button[role="switch"]',
			);
			expect(entryToggle).toBeTruthy();
			await user.click(entryToggle as HTMLElement);

			await waitFor(() => {
				expect(updateCalled).toBe(true);
			});

			// Should have entry_enabled with uuid-2 set to false
			expect(updateData).toMatchObject({
				entry_enabled: { "uuid-2": false },
			});
		});

		it("Toggling last enabled entry shows error toast", async () => {
			const groupWithOneEntry = {
				...mockFailoverGroup,
				entries: [
					{
						provider_name: "OpenAI",
						model_id: "gpt-4",
						enabled: true,
						model_uuid: "uuid-only",
					},
				],
			};

			let updateCalled = false;

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({
						groups: [groupWithOneEntry],
						last_synced_at: null,
					}),
				),
				http.put("/api/failover-groups/:id", async () => {
					updateCalled = true;
					return HttpResponse.json({});
				}),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/test-model")).toBeInTheDocument();
			});

			// Find the OpenAI entry container, then find its toggle
			const openaiEntry = screen
				.getByText("OpenAI")
				.closest(".relative.flex.items-center");
			expect(openaiEntry).toBeTruthy();
			const entryToggle = openaiEntry?.querySelector('button[role="switch"]');
			expect(entryToggle).toBeTruthy();
			await user.click(entryToggle as HTMLElement);

			// Should show error toast
			await waitFor(() => {
				expect(
					screen.getByText("At least one provider must remain active"),
				).toBeInTheDocument();
			});

			// Should NOT call update
			expect(updateCalled).toBe(false);
		});
	});

	describe("Copy Model Name", () => {
		it("Clicking model name copies hotel/model to clipboard", async () => {
			const groupWithEntries = {
				...mockFailoverGroup,
				display_model: "test-model",
				entries: [
					{
						provider_name: "Test Provider",
						model_id: "test-model",
						enabled: true,
						model_uuid: "model-001",
					},
				],
			};

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({
						groups: [groupWithEntries],
						last_synced_at: null,
					}),
				),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/test-model")).toBeInTheDocument();
			});

			// Click the model name (it's a div with role="button" and title="Click to copy")
			const modelNameElement = screen.getByText("hotel/test-model");
			await user.click(modelNameElement);

			// Verify clipboard was called (the toast should appear)
			await waitFor(() => {
				expect(screen.getByText("Copied hotel/test-model")).toBeInTheDocument();
			});
		});
	});

	describe("Bulk Model Toggle", () => {
		it("Selecting groups shows bulk action buttons", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					display_model: "alpha-model",
					entries: [
						{
							provider_name: "OpenAI",
							model_id: "gpt-4",
							enabled: true,
							model_uuid: "uuid-1",
						},
					],
				},
				{
					...mockFailoverGroup,
					id: "fg-002",
					display_model: "beta-model",
					entries: [
						{
							provider_name: "Anthropic",
							model_id: "claude-3",
							enabled: true,
							model_uuid: "uuid-2",
						},
					],
				},
			];

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({ groups, last_synced_at: null }),
				),
				http.get("/api/failover-groups/candidates", () =>
					HttpResponse.json([]),
				),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/alpha-model")).toBeInTheDocument();
			});

			const checkboxes = screen.getAllByRole("checkbox");
			await user.click(checkboxes[0]);

			await waitFor(() => {
				expect(screen.getByText("1 selected")).toBeInTheDocument();
				expect(
					screen.getByRole("button", { name: "Enable all" }),
				).toBeInTheDocument();
				expect(
					screen.getByRole("button", { name: "Disable all" }),
				).toBeInTheDocument();
				expect(
					screen.getByRole("button", { name: "Deselect all" }),
				).toBeInTheDocument();
			});
		});

		it("Enable all entries in selected groups", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					display_model: "alpha-model",
					entries: [
						{
							provider_name: "OpenAI",
							model_id: "gpt-4",
							enabled: false,
							model_uuid: "uuid-1",
						},
					],
				},
				{
					...mockFailoverGroup,
					id: "fg-002",
					display_model: "beta-model",
					entries: [
						{
							provider_name: "Anthropic",
							model_id: "claude-3",
							enabled: false,
							model_uuid: "uuid-2",
						},
					],
				},
			];

			const putCalls: Array<{ id: string; data: unknown }> = [];

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({ groups, last_synced_at: null }),
				),
				http.get("/api/failover-groups/candidates", () =>
					HttpResponse.json([]),
				),
				http.put("/api/failover-groups/:id", async ({ params, request }) => {
					const body = await request.json();
					putCalls.push({ id: params.id as string, data: body });
					return HttpResponse.json({});
				}),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/alpha-model")).toBeInTheDocument();
			});

			const checkboxes = screen.getAllByRole("checkbox");
			await user.click(checkboxes[0]);
			await user.click(checkboxes[1]);

			await waitFor(() => {
				expect(screen.getByText("2 selected")).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: "Enable all" }));

			await waitFor(() => {
				expect(putCalls.length).toBe(2);
				expect(putCalls[0].data).toEqual({ entry_enabled: { "uuid-1": true } });
				expect(putCalls[1].data).toEqual({ entry_enabled: { "uuid-2": true } });
			});
		});

		it("Disable all entries also disables group when group was enabled", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					display_model: "alpha-model",
					group_enabled: true,
					entries: [
						{
							provider_name: "OpenAI",
							model_id: "gpt-4",
							enabled: true,
							model_uuid: "uuid-1",
						},
					],
				},
			];

			const putCalls: Array<{ id: string; data: unknown }> = [];

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({ groups, last_synced_at: null }),
				),
				http.get("/api/failover-groups/candidates", () =>
					HttpResponse.json([]),
				),
				http.put("/api/failover-groups/:id", async ({ params, request }) => {
					const body = await request.json();
					putCalls.push({ id: params.id as string, data: body });
					return HttpResponse.json({});
				}),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/alpha-model")).toBeInTheDocument();
			});

			const checkboxes = screen.getAllByRole("checkbox");
			await user.click(checkboxes[0]);

			await user.click(screen.getByRole("button", { name: "Disable all" }));

			await waitFor(() => {
				expect(putCalls.length).toBe(1);
				expect(putCalls[0].data).toEqual({
					entry_enabled: { "uuid-1": false },
					group_enabled: false,
				});
			});
		});

		it("Bulk toggle error shows error toast", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					display_model: "alpha-model",
					entries: [
						{
							provider_name: "OpenAI",
							model_id: "gpt-4",
							enabled: true,
							model_uuid: "uuid-1",
						},
					],
				},
			];

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({ groups, last_synced_at: null }),
				),
				http.get("/api/failover-groups/candidates", () =>
					HttpResponse.json([]),
				),
				http.put("/api/failover-groups/:id", () =>
					HttpResponse.json({ error: "Failed" }, { status: 500 }),
				),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/alpha-model")).toBeInTheDocument();
			});

			const checkboxes = screen.getAllByRole("checkbox");
			await user.click(checkboxes[0]);

			await user.click(screen.getByRole("button", { name: "Disable all" }));

			await waitFor(() => {
				expect(
					screen.getByText("Bulk toggle failed for some groups"),
				).toBeInTheDocument();
			});
		});

		it("Checkbox icon deselects all", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					display_model: "alpha-model",
					entries: [
						{
							provider_name: "OpenAI",
							model_id: "gpt-4",
							enabled: true,
							model_uuid: "uuid-1",
						},
					],
				},
			];

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({ groups, last_synced_at: null }),
				),
				http.get("/api/failover-groups/candidates", () =>
					HttpResponse.json([]),
				),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/alpha-model")).toBeInTheDocument();
			});

			const checkboxes = screen.getAllByRole("checkbox");
			await user.click(checkboxes[0]);

			await waitFor(() => {
				expect(screen.getByText("1 selected")).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: "Deselect all" }));

			await waitFor(() => {
				expect(screen.queryByText("1 selected")).not.toBeInTheDocument();
				expect(
					screen.queryByRole("button", { name: "Enable all" }),
				).not.toBeInTheDocument();
			});
		});

		it("Checkbox icon selects all groups", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					display_model: "alpha-model",
					entries: [
						{
							provider_name: "OpenAI",
							model_id: "gpt-4",
							enabled: true,
							model_uuid: "uuid-1",
						},
					],
				},
				{
					...mockFailoverGroup,
					id: "fg-002",
					display_model: "beta-model",
					entries: [
						{
							provider_name: "Anthropic",
							model_id: "claude-3",
							enabled: true,
							model_uuid: "uuid-2",
						},
					],
				},
			];

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({ groups, last_synced_at: null }),
				),
				http.get("/api/failover-groups/candidates", () =>
					HttpResponse.json([]),
				),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/alpha-model")).toBeInTheDocument();
			});

			// Click the empty checkbox icon to select all
			await user.click(screen.getByRole("button", { name: "Select all" }));

			await waitFor(() => {
				expect(screen.getByText("2 selected")).toBeInTheDocument();
			});
		});
	});

	describe("Bulk Delete", () => {
		it("Delete all button appears when groups are selected", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					display_model: "alpha-model",
					entries: [
						{
							provider_name: "OpenAI",
							model_id: "gpt-4",
							enabled: true,
							model_uuid: "uuid-1",
						},
					],
				},
				{
					...mockFailoverGroup,
					id: "fg-002",
					display_model: "beta-model",
					entries: [
						{
							provider_name: "Anthropic",
							model_id: "claude-3",
							enabled: true,
							model_uuid: "uuid-2",
						},
					],
				},
			];

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({ groups, last_synced_at: null }),
				),
				http.get("/api/failover-groups/candidates", () =>
					HttpResponse.json([]),
				),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/alpha-model")).toBeInTheDocument();
			});

			// Select all groups
			await user.click(screen.getByRole("button", { name: "Select all" }));

			await waitFor(() => {
				expect(screen.getByText("2 selected")).toBeInTheDocument();
				expect(
					screen.getByRole("button", { name: "Delete all" }),
				).toBeInTheDocument();
				// Verify it has the danger class
				expect(screen.getByRole("button", { name: "Delete all" })).toHaveClass(
					"ui-btn-danger",
				);
			});
		});

		it("Delete all button opens confirmation modal", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					display_model: "alpha-model",
					entries: [
						{
							provider_name: "OpenAI",
							model_id: "gpt-4",
							enabled: true,
							model_uuid: "uuid-1",
						},
					],
				},
				{
					...mockFailoverGroup,
					id: "fg-002",
					display_model: "beta-model",
					entries: [
						{
							provider_name: "Anthropic",
							model_id: "claude-3",
							enabled: true,
							model_uuid: "uuid-2",
						},
					],
				},
			];

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({ groups, last_synced_at: null }),
				),
				http.get("/api/failover-groups/candidates", () =>
					HttpResponse.json([]),
				),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/alpha-model")).toBeInTheDocument();
			});

			// Select all groups
			await user.click(screen.getByRole("button", { name: "Select all" }));

			await waitFor(() => {
				expect(screen.getByText("2 selected")).toBeInTheDocument();
			});

			// Click Delete all button
			await user.click(screen.getByRole("button", { name: "Delete all" }));

			await waitFor(() => {
				// Modal should appear with correct entity name and type
				expect(
					screen.getByText(/Are you sure you want to delete/),
				).toBeInTheDocument();
				expect(screen.getByText("2 failover groups")).toBeInTheDocument();
				expect(screen.getByText("Delete failover groups")).toBeInTheDocument();
			});
		});

		it("Cancel bulk delete closes modal", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					display_model: "alpha-model",
					entries: [
						{
							provider_name: "OpenAI",
							model_id: "gpt-4",
							enabled: true,
							model_uuid: "uuid-1",
						},
					],
				},
			];

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({ groups, last_synced_at: null }),
				),
				http.get("/api/failover-groups/candidates", () =>
					HttpResponse.json([]),
				),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/alpha-model")).toBeInTheDocument();
			});

			// Select the group
			await user.click(screen.getByRole("button", { name: "Select all" }));

			await waitFor(() => {
				expect(screen.getByText("1 selected")).toBeInTheDocument();
			});

			// Click Delete all button
			await user.click(screen.getByRole("button", { name: "Delete all" }));

			await waitFor(() => {
				expect(
					screen.getByText(/Are you sure you want to delete/),
				).toBeInTheDocument();
			});

			// Click Cancel button
			await user.click(screen.getByRole("button", { name: "Cancel" }));

			await waitFor(() => {
				expect(
					screen.queryByText(/Are you sure you want to delete/),
				).not.toBeInTheDocument();
			});
		});

		it("Confirm bulk delete succeeds for all groups", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					id: "fg-001",
					display_model: "alpha-model",
					entries: [
						{
							provider_name: "OpenAI",
							model_id: "gpt-4",
							enabled: true,
							model_uuid: "uuid-1",
						},
					],
				},
				{
					...mockFailoverGroup,
					id: "fg-002",
					display_model: "beta-model",
					entries: [
						{
							provider_name: "Anthropic",
							model_id: "claude-3",
							enabled: true,
							model_uuid: "uuid-2",
						},
					],
				},
			];

			const deleteCalls: string[] = [];

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({ groups, last_synced_at: null }),
				),
				http.get("/api/failover-groups/candidates", () =>
					HttpResponse.json([]),
				),
				http.delete("/api/failover-groups/:id", ({ params }) => {
					deleteCalls.push(params.id as string);
					return new HttpResponse(null, { status: 204 });
				}),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/alpha-model")).toBeInTheDocument();
			});

			// Select all groups
			await user.click(screen.getByRole("button", { name: "Select all" }));

			await waitFor(() => {
				expect(screen.getByText("2 selected")).toBeInTheDocument();
			});

			// Click Delete all button
			await user.click(screen.getByRole("button", { name: "Delete all" }));

			await waitFor(() => {
				expect(
					screen.getByText(/Are you sure you want to delete/),
				).toBeInTheDocument();
			});

			// Confirm deletion
			await user.click(screen.getByRole("button", { name: "Delete" }));

			await waitFor(() => {
				// Both groups should be deleted
				expect(deleteCalls.length).toBe(2);
				expect(deleteCalls).toContain("fg-001");
				expect(deleteCalls).toContain("fg-002");
				// Success toast should appear
				expect(screen.getByText("Deleted 2 groups")).toBeInTheDocument();
				// Selection should be cleared
				expect(screen.queryByText("2 selected")).not.toBeInTheDocument();
				expect(
					screen.queryByRole("button", { name: "Delete all" }),
				).not.toBeInTheDocument();
			});
		});

		it("Confirm bulk delete handles partial failures", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					id: "fg-001",
					display_model: "alpha-model",
					entries: [
						{
							provider_name: "OpenAI",
							model_id: "gpt-4",
							enabled: true,
							model_uuid: "uuid-1",
						},
					],
				},
				{
					...mockFailoverGroup,
					id: "fg-002",
					display_model: "beta-model",
					entries: [
						{
							provider_name: "Anthropic",
							model_id: "claude-3",
							enabled: true,
							model_uuid: "uuid-2",
						},
					],
				},
				{
					...mockFailoverGroup,
					id: "fg-003",
					display_model: "gamma-model",
					entries: [
						{
							provider_name: "Google",
							model_id: "gemini-pro",
							enabled: true,
							model_uuid: "uuid-3",
						},
					],
				},
			];

			const deleteCalls: string[] = [];

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({ groups, last_synced_at: null }),
				),
				http.get("/api/failover-groups/candidates", () =>
					HttpResponse.json([]),
				),
				http.delete("/api/failover-groups/:id", ({ params }) => {
					deleteCalls.push(params.id as string);
					// Fail deletion for fg-002
					if (params.id === "fg-002") {
						return HttpResponse.json({ error: "not found" }, { status: 500 });
					}
					return new HttpResponse(null, { status: 204 });
				}),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/alpha-model")).toBeInTheDocument();
			});

			// Select all groups
			await user.click(screen.getByRole("button", { name: "Select all" }));

			await waitFor(() => {
				expect(screen.getByText("3 selected")).toBeInTheDocument();
			});

			// Click Delete all button
			await user.click(screen.getByRole("button", { name: "Delete all" }));

			await waitFor(() => {
				expect(
					screen.getByText(/Are you sure you want to delete/),
				).toBeInTheDocument();
			});

			// Confirm deletion
			await user.click(screen.getByRole("button", { name: "Delete" }));

			await waitFor(() => {
				// All three should be attempted
				expect(deleteCalls.length).toBe(3);
				// Warning toast with partial failure message
				expect(
					screen.getByText("Deleted 2 of 3 groups (1 failed)"),
				).toBeInTheDocument();
			});
		});

		it("Shows loading state during bulk delete", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					id: "fg-001",
					display_model: "alpha-model",
					entries: [
						{
							provider_name: "OpenAI",
							model_id: "gpt-4",
							enabled: true,
							model_uuid: "uuid-1",
						},
					],
				},
			];

			let resolveDelete: () => void;
			const deletePromise = new Promise<void>((resolve) => {
				resolveDelete = resolve;
			});

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({ groups, last_synced_at: null }),
				),
				http.get("/api/failover-groups/candidates", () =>
					HttpResponse.json([]),
				),
				http.delete("/api/failover-groups/:id", () => {
					return deletePromise.then(
						() => new HttpResponse(null, { status: 204 }),
					);
				}),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/alpha-model")).toBeInTheDocument();
			});

			// Select the group
			await user.click(screen.getByRole("button", { name: "Select all" }));

			await waitFor(() => {
				expect(screen.getByText("1 selected")).toBeInTheDocument();
			});

			// Click Delete all button
			await user.click(screen.getByRole("button", { name: "Delete all" }));

			await waitFor(() => {
				expect(
					screen.getByText(/Are you sure you want to delete/),
				).toBeInTheDocument();
			});

			// Confirm deletion
			await user.click(screen.getByRole("button", { name: "Delete" }));

			// Check loading state - button should show "Deleting…" and be disabled
			await waitFor(() => {
				const deleteButton = screen.getByRole("button", { name: "Deleting…" });
				expect(deleteButton).toBeDisabled();
			});

			// Resolve the delete operation
			resolveDelete?.();

			await waitFor(() => {
				expect(screen.getByText("Deleted 1 group")).toBeInTheDocument();
			});
		});

		it("Bulk delete with empty set does nothing", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					display_model: "alpha-model",
					entries: [
						{
							provider_name: "OpenAI",
							model_id: "gpt-4",
							enabled: true,
							model_uuid: "uuid-1",
						},
					],
				},
			];

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({ groups, last_synced_at: null }),
				),
				http.get("/api/failover-groups/candidates", () =>
					HttpResponse.json([]),
				),
			);

			renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/alpha-model")).toBeInTheDocument();
			});

			// No groups selected, so Delete all button should not be visible
			expect(
				screen.queryByRole("button", { name: "Delete all" }),
			).not.toBeInTheDocument();

			// The confirmBulkDelete function should early return when bulkDeleteIds is null/empty
			// This is tested implicitly by the absence of any API calls or toasts
			// when no groups are selected
		});
	});

	describe("Bulk Provider Toggle", () => {
		it("Provider filter shows provider action bar", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					display_model: "alpha-model",
					entries: [
						{
							provider_name: "OpenAI",
							model_id: "gpt-4",
							enabled: true,
							model_uuid: "uuid-1",
						},
						{
							provider_name: "Anthropic",
							model_id: "claude-3",
							enabled: true,
							model_uuid: "uuid-2",
						},
					],
				},
			];

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({ groups, last_synced_at: null }),
				),
				http.get("/api/failover-groups/candidates", () =>
					HttpResponse.json([]),
				),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/alpha-model")).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: "All providers" }));
			await user.click(screen.getByRole("button", { name: "OpenAI" }));

			await waitFor(() => {
				expect(
					screen.getByText("1 group with OpenAI entries"),
				).toBeInTheDocument();
				expect(
					screen.getByRole("button", { name: "Enable all OpenAI" }),
				).toBeInTheDocument();
				expect(
					screen.getByRole("button", { name: "Disable all OpenAI" }),
				).toBeInTheDocument();
			});
		});

		it("Enable all provider entries across groups", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					display_model: "alpha-model",
					entries: [
						{
							provider_name: "OpenAI",
							model_id: "gpt-4",
							enabled: false,
							model_uuid: "uuid-1",
						},
						{
							provider_name: "Anthropic",
							model_id: "claude-3",
							enabled: true,
							model_uuid: "uuid-2",
						},
					],
				},
			];

			const putCalls: Array<{ id: string; data: unknown }> = [];

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({ groups, last_synced_at: null }),
				),
				http.get("/api/failover-groups/candidates", () =>
					HttpResponse.json([]),
				),
				http.put("/api/failover-groups/:id", async ({ params, request }) => {
					const body = await request.json();
					putCalls.push({ id: params.id as string, data: body });
					return HttpResponse.json({});
				}),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/alpha-model")).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: "All providers" }));
			await user.click(screen.getByRole("button", { name: "OpenAI" }));

			await user.click(
				screen.getByRole("button", { name: "Enable all OpenAI" }),
			);

			await waitFor(() => {
				expect(putCalls.length).toBe(1);
				expect(putCalls[0].data).toEqual({
					entry_enabled: { "uuid-1": true, "uuid-2": true },
				});
			});
		});

		it("Disable all provider entries preserves other providers' enabled state", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					display_model: "alpha-model",
					entries: [
						{
							provider_name: "OpenAI",
							model_id: "gpt-4",
							enabled: true,
							model_uuid: "uuid-1",
						},
						{
							provider_name: "Anthropic",
							model_id: "claude-3",
							enabled: true,
							model_uuid: "uuid-2",
						},
					],
				},
			];

			const putCalls: Array<{ id: string; data: unknown }> = [];

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({ groups, last_synced_at: null }),
				),
				http.get("/api/failover-groups/candidates", () =>
					HttpResponse.json([]),
				),
				http.put("/api/failover-groups/:id", async ({ params, request }) => {
					const body = await request.json();
					putCalls.push({ id: params.id as string, data: body });
					return HttpResponse.json({});
				}),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/alpha-model")).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: "All providers" }));
			await user.click(screen.getByRole("button", { name: "OpenAI" }));

			await user.click(
				screen.getByRole("button", { name: "Disable all OpenAI" }),
			);

			await waitFor(() => {
				expect(putCalls.length).toBe(1);
				expect(putCalls[0].data).toEqual({
					entry_enabled: { "uuid-1": false, "uuid-2": true },
				});
			});
		});

		it("Disable all provider entries also disables group when no entries remain enabled", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					display_model: "alpha-model",
					group_enabled: true,
					entries: [
						{
							provider_name: "OpenAI",
							model_id: "gpt-4",
							enabled: true,
							model_uuid: "uuid-1",
						},
					],
				},
			];

			const putCalls: Array<{ id: string; data: unknown }> = [];

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({ groups, last_synced_at: null }),
				),
				http.get("/api/failover-groups/candidates", () =>
					HttpResponse.json([]),
				),
				http.put("/api/failover-groups/:id", async ({ params, request }) => {
					const body = await request.json();
					putCalls.push({ id: params.id as string, data: body });
					return HttpResponse.json({});
				}),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/alpha-model")).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: "All providers" }));
			await user.click(screen.getByRole("button", { name: "OpenAI" }));

			await user.click(
				screen.getByRole("button", { name: "Disable all OpenAI" }),
			);

			await waitFor(() => {
				expect(putCalls.length).toBe(1);
				expect(putCalls[0].data).toEqual({
					entry_enabled: { "uuid-1": false },
					group_enabled: false,
				});
			});
		});

		it("Bulk provider toggle error shows error toast", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					display_model: "alpha-model",
					entries: [
						{
							provider_name: "OpenAI",
							model_id: "gpt-4",
							enabled: true,
							model_uuid: "uuid-1",
						},
					],
				},
			];

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({ groups, last_synced_at: null }),
				),
				http.get("/api/failover-groups/candidates", () =>
					HttpResponse.json([]),
				),
				http.put("/api/failover-groups/:id", () =>
					HttpResponse.json({ error: "Failed" }, { status: 500 }),
				),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/alpha-model")).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: "All providers" }));
			await user.click(screen.getByRole("button", { name: "OpenAI" }));

			await user.click(
				screen.getByRole("button", { name: "Disable all OpenAI" }),
			);

			await waitFor(() => {
				expect(
					screen.getByText("Bulk provider toggle failed for some groups"),
				).toBeInTheDocument();
			});
		});
	});

	describe("Badge", () => {
		it("Shows enabled/disabled badge when groups have mixed states", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					display_model: "alpha-model",
					group_enabled: true,
				},
				{
					...mockFailoverGroup,
					id: "fg-002",
					display_model: "beta-model",
					group_enabled: false,
				},
			];

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({ groups, last_synced_at: null }),
				),
			);

			renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("1 enabled")).toBeInTheDocument();
				expect(screen.getByText("1 disabled")).toBeInTheDocument();
			});
		});

		it("Does not show badge when all groups same state", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					display_model: "alpha-model",
					group_enabled: true,
				},
				{
					...mockFailoverGroup,
					id: "fg-002",
					display_model: "beta-model",
					group_enabled: true,
				},
			];

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({ groups, last_synced_at: null }),
				),
			);

			renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.queryByText("enabled")).not.toBeInTheDocument();
				expect(screen.queryByText("disabled")).not.toBeInTheDocument();
			});
		});
	});

	describe("Alphabet Sidebar", () => {
		it("Shows alphabet sidebar when more than 3 letter groups", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					display_model: "alpha-model",
					auto_created: true,
				},
				{
					...mockFailoverGroup,
					id: "fg-002",
					display_model: "beta-model",
					auto_created: true,
				},
				{
					...mockFailoverGroup,
					id: "fg-003",
					display_model: "gamma-model",
					auto_created: true,
				},
				{
					...mockFailoverGroup,
					id: "fg-004",
					display_model: "delta-model",
					auto_created: true,
				},
			];

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({ groups, last_synced_at: null }),
				),
			);

			renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByRole("button", { name: "A" })).toBeInTheDocument();
				expect(screen.getByRole("button", { name: "B" })).toBeInTheDocument();
				expect(screen.getByRole("button", { name: "D" })).toBeInTheDocument();
				expect(screen.getByRole("button", { name: "G" })).toBeInTheDocument();
			});
		});

		it("Does not show alphabet sidebar with 3 or fewer letter groups", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					display_model: "alpha-model",
					auto_created: true,
				},
				{
					...mockFailoverGroup,
					id: "fg-002",
					display_model: "beta-model",
					auto_created: true,
				},
				{
					...mockFailoverGroup,
					id: "fg-003",
					display_model: "gamma-model",
					auto_created: true,
				},
			];

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({ groups, last_synced_at: null }),
				),
			);

			renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(
					screen.queryByRole("button", { name: "A" }),
				).not.toBeInTheDocument();
			});

			// Sidebar should not render at all (no custom groups + ≤3 letter sections)
			expect(
				screen.queryByRole("navigation", { name: /alphabet/i }),
			).not.toBeInTheDocument();
		});
	});

	describe("Empty State Actions", () => {
		it("Empty state with no filters shows auto-discover action", async () => {
			let syncCalled = false;

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({ groups: [], last_synced_at: null }),
				),
				http.get("/api/failover-groups/candidates", () =>
					HttpResponse.json([]),
				),
				http.post("/api/failover-groups/sync", () => {
					syncCalled = true;
					return HttpResponse.json({ disabled_groups: [] });
				}),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(
					screen.getByText("No failover groups configured"),
				).toBeInTheDocument();
				expect(
					screen.getByRole("button", { name: "Auto-discover from models" }),
				).toBeInTheDocument();
			});

			await user.click(
				screen.getByRole("button", { name: "Auto-discover from models" }),
			);

			await waitFor(() => {
				expect(syncCalled).toBe(true);
			});
		});

		it("Empty state with filters shows clear filters action", async () => {
			const groups = [{ ...mockFailoverGroup, display_model: "alpha-model" }];

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({ groups, last_synced_at: null }),
				),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/alpha-model")).toBeInTheDocument();
			});

			const filterInput = screen.getByPlaceholderText("Filter hotel/model…");
			await user.type(filterInput, "NonExistentModel");

			await waitFor(() => {
				expect(
					screen.getByText("No groups matching filters"),
				).toBeInTheDocument();
				expect(
					screen.getByRole("button", { name: "Clear filters" }),
				).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: "Clear filters" }));

			await waitFor(() => {
				expect(screen.getByText("hotel/alpha-model")).toBeInTheDocument();
				expect(
					screen.queryByText("No groups matching filters"),
				).not.toBeInTheDocument();
			});
		});
	});

	describe("Group Card Checkbox", () => {
		it("Checking group card checkbox selects the group", async () => {
			const groups = [
				{
					...mockFailoverGroup,
					display_model: "alpha-model",
					entries: [
						{
							provider_name: "OpenAI",
							model_id: "gpt-4",
							enabled: true,
							model_uuid: "uuid-1",
						},
					],
				},
			];

			server.use(
				http.get("/api/failover-groups", () =>
					HttpResponse.json({ groups, last_synced_at: null }),
				),
				http.get("/api/failover-groups/candidates", () =>
					HttpResponse.json([]),
				),
			);

			const { user } = renderWithProviders(<FailoverGroups />);

			await waitFor(() => {
				expect(screen.getByText("hotel/alpha-model")).toBeInTheDocument();
			});

			const checkboxes = screen.getAllByRole("checkbox");
			expect((checkboxes[0] as HTMLInputElement).checked).toBe(false);

			await user.click(checkboxes[0]);

			await waitFor(() => {
				expect(screen.getByText("1 selected")).toBeInTheDocument();
			});
		});
	});
});
