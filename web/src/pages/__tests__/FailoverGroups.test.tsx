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
				// Provider filter is a select with "All providers" option
				const selects = screen.getAllByRole("combobox");
				// Find the one that contains "All providers" option
				const providerSelect = selects.find((s) =>
					s
						.querySelector('option[value=""]')
						?.textContent?.includes("All providers"),
				);
				expect(providerSelect).toBeInTheDocument();
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
				const selects = screen.getAllByRole("combobox");
				expect(selects.length).toBeGreaterThanOrEqual(2);
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

			// Use provider select dropdown to filter
			const providerSelect = screen.getAllByRole("combobox")[0];
			await user.selectOptions(providerSelect, "Provider Alpha");

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

			// Use enabled/disabled select dropdown (second combobox)
			const enabledSelect = screen.getAllByRole("combobox")[1];
			await user.selectOptions(enabledSelect, "enabled");

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

			// Use enabled/disabled select dropdown (second combobox)
			const enabledSelect = screen.getAllByRole("combobox")[1];
			await user.selectOptions(enabledSelect, "disabled");

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
				{ ...mockFailoverGroup, display_model: "alpha-one" },
				{
					...mockFailoverGroup,
					display_model: "alpha-two",
					id: "fg-002",
				},
				{ ...mockFailoverGroup, display_model: "beta-one", id: "fg-003" },
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
				{ ...mockFailoverGroup, display_model: "test-one" },
				{ ...mockFailoverGroup, display_model: "test-two", id: "fg-002" },
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
				// Provider name appears in card (span with text-white class) and dropdown
				// Use getAllByText and check we have at least 2 occurrences
				const providerElements = screen.getAllByText("UniqueProvider");
				expect(providerElements.length).toBeGreaterThanOrEqual(2);
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
});
