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
			expect(screen.getByText("Loading…")).toBeInTheDocument();
		});
	});

	describe("Section collapsing", () => {
		it("rotates the section chevrons when custom and letter sections collapse", async () => {
			const autoGroup = {
				...mockFailoverGroup,
				id: "fg-auto",
				display_model: "auto-model",
				display_name: "Auto Group",
				auto_created: true,
			};
			server.use(
				http.get("/api/failover-groups", () => {
					return HttpResponse.json({
						groups: [mockFailoverGroup, autoGroup],
						last_synced_at: null,
					});
				}),
			);

			const { user } = renderWithProviders(<FailoverGroups />);
			await waitFor(() => {
				expect(
					document.querySelector("#failover-section-custom"),
				).toBeInTheDocument();
			});

			for (const sectionId of [
				"failover-section-custom",
				"failover-section-A",
			]) {
				const section = document.querySelector(`#${sectionId}`);
				expect(section).toBeInTheDocument();
				const header = section?.querySelector("button");
				const chevron = header?.querySelector("svg");
				// Expanded by default: chevron points down (rotated)
				expect(chevron).toHaveClass("rotate-90");
				if (header) await user.click(header);
				expect(chevron).not.toHaveClass("rotate-90");
			}
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
					screen.getByRole("button", { name: "New Group" }),
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
					return HttpResponse.json({ deleted_groups: [] });
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
