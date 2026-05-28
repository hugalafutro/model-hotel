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
					return HttpResponse.json({ deleted_groups: [] });
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
					HttpResponse.json({ deleted_groups: [] }),
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
						deleted_groups: [
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
					screen.getByText("hotel/gpt-4 deleted: no providers (OpenAI)"),
				).toBeInTheDocument();
			});
		});

		it("Sync success with purged entries shows info toast", async () => {
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
						deleted_groups: [],
						purged_entries: [
							{
								group_display_model: "claude-3",
								pruned_model_ids: ["uuid-a", "uuid-b"],
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
					screen.getByText("hotel/claude-3: removed 2 stale entry(ies)"),
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
							resolve(HttpResponse.json({ deleted_groups: [] }));
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
});
