import { screen, waitFor, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import { mockFailoverGroup } from "../../test/mocks/data";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { FailoverGroups } from "../FailoverGroups";

function getModalDeleteButton(): HTMLElement {
	const dialog = screen.getByRole("dialog");
	return within(dialog).getByRole("button", { name: "Delete" });
}

describe("FailoverGroups", () => {
	beforeEach(() => {
		server.resetHandlers();
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

			// Click Delete button on the group card
			const deleteButton = screen.getByRole("button", { name: "Delete" });
			await user.click(deleteButton);

			await waitFor(() => {
				expect(
					screen.getByText(/Are you sure you want to delete/),
				).toBeInTheDocument();
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
			await user.click(screen.getAllByRole("button", { name: "Delete" })[0]);

			// Wait for modal to open
			await waitFor(() => {
				expect(
					screen.getByText(/Are you sure you want to delete/),
				).toBeInTheDocument();
			});

			// Click confirm button (modal Delete button has ui-btn-danger class)
			await user.click(getModalDeleteButton());

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

			await user.click(screen.getAllByRole("button", { name: "Delete" })[0]);

			await waitFor(() => {
				expect(
					screen.getByText(/Are you sure you want to delete/),
				).toBeInTheDocument();
			});

			// Click confirm button (modal Delete button has ui-btn-danger class)
			await user.click(getModalDeleteButton());

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

			await user.click(screen.getAllByRole("button", { name: "Delete" })[0]);

			await waitFor(() => {
				expect(
					screen.getByText(/Are you sure you want to delete/),
				).toBeInTheDocument();
			});

			// Click confirm button (modal Delete button has ui-btn-danger class)
			await user.click(getModalDeleteButton());

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

			await user.click(screen.getAllByRole("button", { name: "Delete" })[0]);

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
				expect(screen.getByText(/\d+ failover groups/)).toBeInTheDocument();
				expect(screen.getByText(/Delete failover groups/i)).toBeInTheDocument();
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

			// Confirm deletion (modal Delete button has ui-btn-danger class)
			await user.click(getModalDeleteButton());

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

			// Confirm deletion (modal Delete button has ui-btn-danger class)
			await user.click(getModalDeleteButton());

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

			let resolveDelete: (() => void) | undefined;
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

			// Confirm deletion (modal Delete button has ui-btn-danger class)
			await user.click(getModalDeleteButton());

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
});
