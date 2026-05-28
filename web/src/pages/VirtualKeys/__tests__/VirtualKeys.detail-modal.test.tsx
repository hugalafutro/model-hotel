import { screen, waitFor, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { mockVirtualKey } from "../../../test/mocks/data";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { VirtualKeys } from "../../VirtualKeys";

describe("VirtualKeys", () => {
	beforeEach(() => {
		server.resetHandlers();
		vi.clearAllMocks();
	});

	describe("Key Detail Modal", () => {
		it("displays key details correctly", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

			// Click on name to open detail modal
			const nameCell = screen.getByText("Test API Key");
			await user.click(nameCell);

			await waitFor(() => {
				expect(
					screen.getByRole("dialog", { name: "Virtual Key Details" }),
				).toBeInTheDocument();
			});

			const dialog = screen.getByRole("dialog", {
				name: "Virtual Key Details",
			});
			expect(within(dialog).getByText("Test API Key")).toBeInTheDocument();
			expect(within(dialog).getByText("sk_test_••••")).toBeInTheDocument();
			expect(within(dialog).getByText("30")).toBeInTheDocument();
			expect(within(dialog).getByText("60")).toBeInTheDocument();
			expect(within(dialog).getByText("50,000")).toBeInTheDocument();
			expect(
				within(dialog).getByText(
					new Date(mockVirtualKey.created_at).toLocaleString(),
				),
			).toBeInTheDocument();
			// mockVirtualKey.last_used_at is "2026-05-11T08:00:00Z" in test data
			expect(
				within(dialog).getByText(
					new Date(mockVirtualKey.last_used_at as string).toLocaleString(),
				),
			).toBeInTheDocument();
		});

		it("shows 'Never' for last used when null", async () => {
			const mockKeyWithoutUsage = {
				...mockVirtualKey,
				id: "vk-no-usage",
				last_used_at: null,
			};

			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockKeyWithoutUsage]),
				),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

			// Click row to open detail modal
			const nameCell = screen.getByText("Test API Key");
			await user.click(nameCell);

			await waitFor(() => {
				expect(
					screen.getByRole("dialog", { name: "Virtual Key Details" }),
				).toBeInTheDocument();
			});

			// Check modal shows "Never" for last used - query within modal
			const modal = screen.getByRole("dialog", {
				name: "Virtual Key Details",
			});
			expect(within(modal).getByText("Never")).toBeInTheDocument();
		});

		it("edits key from detail modal and saves successfully", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
				http.put("/api/virtual-keys/vk-001", async ({ request }) => {
					const body = await request.json();
					return HttpResponse.json({
						...mockVirtualKey,
						name: (body as { name: string }).name,
					});
				}),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

			// Click on name to open detail modal
			const nameCell = screen.getByText("Test API Key");
			await user.click(nameCell);

			await waitFor(() => {
				expect(
					screen.getByRole("dialog", { name: "Virtual Key Details" }),
				).toBeInTheDocument();
			});

			const dialog = screen.getByRole("dialog", {
				name: "Virtual Key Details",
			});

			// Click Edit button to enter edit mode
			const editButton = within(dialog).getByRole("button", {
				name: "Edit",
			});
			await user.click(editButton);

			// Update name
			const nameInput = within(dialog).getByLabelText("Name");
			await user.clear(nameInput);
			await user.type(nameInput, "Updated Key Name");

			// Click Save Changes
			const saveButton = within(dialog).getByRole("button", {
				name: "Save Changes",
			});
			await user.click(saveButton);

			await waitFor(() => {
				expect(screen.getByText("Virtual key updated")).toBeInTheDocument();
			});

			// Modal closes after successful save
			await waitFor(() => {
				expect(
					screen.queryByRole("dialog", { name: "Virtual Key Details" }),
				).not.toBeInTheDocument();
			});
		});

		it("shows error toast when update fails from detail modal", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
				http.put("/api/virtual-keys/vk-001", () =>
					HttpResponse.json({ error: "Update failed" }, { status: 500 }),
				),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

			// Click on name to open detail modal
			const nameCell = screen.getByText("Test API Key");
			await user.click(nameCell);

			await waitFor(() => {
				expect(
					screen.getByRole("dialog", { name: "Virtual Key Details" }),
				).toBeInTheDocument();
			});

			const dialog = screen.getByRole("dialog", {
				name: "Virtual Key Details",
			});

			// Click Edit button
			const editButton = within(dialog).getByRole("button", {
				name: "Edit",
			});
			await user.click(editButton);

			// Update name
			const nameInput = within(dialog).getByLabelText("Name");
			await user.clear(nameInput);
			await user.type(nameInput, "Updated Key");

			// Click Save Changes
			const saveButton = within(dialog).getByRole("button", {
				name: "Save Changes",
			});
			await user.click(saveButton);

			await waitFor(() => {
				expect(screen.getByText(/Failed:.*Update failed/i)).toBeInTheDocument();
			});
		});

		it("updates rate limits", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
				http.put("/api/virtual-keys/vk-001", async ({ request }) => {
					const body = await request.json();
					return HttpResponse.json({
						...mockVirtualKey,
						rate_limit_rps: (body as { rate_limit_rps: number | null })
							.rate_limit_rps,
						rate_limit_burst: (body as { rate_limit_burst: number | null })
							.rate_limit_burst,
					});
				}),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

			// Click on name to open detail modal
			const nameCell = screen.getByText("Test API Key");
			await user.click(nameCell);

			await waitFor(() => {
				expect(
					screen.getByRole("dialog", { name: "Virtual Key Details" }),
				).toBeInTheDocument();
			});

			const dialog = screen.getByRole("dialog", {
				name: "Virtual Key Details",
			});

			// Click Edit button
			const editButton = within(dialog).getByRole("button", {
				name: "Edit",
			});
			await user.click(editButton);

			// Update RPS
			const rateLimitRpsInput = within(dialog).getByLabelText(
				"Rate Limit RPS (requests/sec)",
			);
			await user.clear(rateLimitRpsInput);
			await user.type(rateLimitRpsInput, "100");

			// Update BURST
			const rateLimitBurstInput = within(dialog).getByLabelText(
				"Rate Limit Burst (max concurrent)",
			);
			await user.clear(rateLimitBurstInput);
			await user.type(rateLimitBurstInput, "200");

			// Click Save Changes
			const saveButton = within(dialog).getByRole("button", {
				name: "Save Changes",
			});
			await user.click(saveButton);

			await waitFor(() => {
				expect(screen.getByText("Virtual key updated")).toBeInTheDocument();
			});
		});

		it("cancels edit and reverts to view mode", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

			// Click on name to open detail modal
			const nameCell = screen.getByText("Test API Key");
			await user.click(nameCell);

			await waitFor(() => {
				expect(
					screen.getByRole("dialog", { name: "Virtual Key Details" }),
				).toBeInTheDocument();
			});

			const dialog = screen.getByRole("dialog", {
				name: "Virtual Key Details",
			});

			// Click Edit button
			const editButton = within(dialog).getByRole("button", {
				name: "Edit",
			});
			await user.click(editButton);

			// Modify name
			const nameInput = within(dialog).getByLabelText("Name");
			await user.clear(nameInput);
			await user.type(nameInput, "Temporary Change");

			// Click Cancel
			const cancelButton = within(dialog).getByRole("button", {
				name: "Cancel",
			});
			await user.click(cancelButton);

			// Should revert to view mode showing original name
			await waitFor(() => {
				expect(within(dialog).getByText("Test API Key")).toBeInTheDocument();
			});
			// Edit button should be visible again (not Save Changes)
			expect(
				within(dialog).getByRole("button", { name: "Edit" }),
			).toBeInTheDocument();
		});

		it("closes edit modal when clicking Cancel", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

			// Click row to open detail modal
			const nameCell = screen.getByText("Test API Key");
			await user.click(nameCell);

			// Wait for detail modal and click Edit
			await waitFor(() => {
				expect(
					screen.getByRole("dialog", { name: "Virtual Key Details" }),
				).toBeInTheDocument();
			});
			await user.click(screen.getByRole("button", { name: "Edit" }));

			// Click Cancel - should return to view mode (modal stays open)
			await user.click(screen.getByRole("button", { name: "Cancel" }));

			// Should still have modal open but in view mode
			await waitFor(() => {
				expect(
					screen.getByRole("dialog", { name: "Virtual Key Details" }),
				).toBeInTheDocument();
				expect(
					screen.getByRole("button", { name: "Edit" }),
				).toBeInTheDocument();
			});
		});

		it("deletes a key successfully", async () => {
			let deleteCalled = false;
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
				http.delete("/api/virtual-keys/vk-001", () => {
					deleteCalled = true;
					return HttpResponse.json({ success: true });
				}),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

			// Click on name to open detail modal
			const nameCell = screen.getByText("Test API Key");
			await user.click(nameCell);

			await waitFor(() => {
				expect(
					screen.getByRole("dialog", { name: "Virtual Key Details" }),
				).toBeInTheDocument();
			});

			const dialog = screen.getByRole("dialog", {
				name: "Virtual Key Details",
			});
			// Click "Delete Key" button (default label)
			const deleteButton = within(dialog).getByRole("button", {
				name: "Delete Key",
			});
			await user.click(deleteButton);

			// Confirm deletion
			await waitFor(() => {
				expect(within(dialog).getByText(/Are you sure/i)).toBeInTheDocument();
			});
			const confirmButton = within(dialog).getByRole("button", {
				name: "Yes, delete",
			});
			await user.click(confirmButton);

			await waitFor(() => {
				expect(screen.getByText("Virtual key deleted")).toBeInTheDocument();
			});
			expect(deleteCalled).toBe(true);

			await waitFor(() => {
				expect(
					screen.queryByRole("dialog", { name: "Virtual Key Details" }),
				).not.toBeInTheDocument();
			});
		});

		it("shows error toast when delete fails", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
				http.delete("/api/virtual-keys/vk-001", () =>
					HttpResponse.json({ error: "Delete failed" }, { status: 500 }),
				),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

			// Click on name to open detail modal
			const nameCell = screen.getByText("Test API Key");
			await user.click(nameCell);

			await waitFor(() => {
				expect(
					screen.getByRole("dialog", { name: "Virtual Key Details" }),
				).toBeInTheDocument();
			});

			const dialog = screen.getByRole("dialog", {
				name: "Virtual Key Details",
			});
			const deleteButton = within(dialog).getByRole("button", {
				name: "Delete Key",
			});
			await user.click(deleteButton);

			await waitFor(() => {
				expect(within(dialog).getByText(/Are you sure/i)).toBeInTheDocument();
			});
			const confirmButton = within(dialog).getByRole("button", {
				name: "Yes, delete",
			});
			await user.click(confirmButton);

			await waitFor(() => {
				expect(
					screen.getByText(/Failed to delete: Failed to delete virtual key/i),
				).toBeInTheDocument();
			});
		});

		it("cancels deletion in confirm state", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

			// Click on name to open detail modal
			const nameCell = screen.getByText("Test API Key");
			await user.click(nameCell);

			await waitFor(() => {
				expect(
					screen.getByRole("dialog", { name: "Virtual Key Details" }),
				).toBeInTheDocument();
			});

			const dialog = screen.getByRole("dialog", {
				name: "Virtual Key Details",
			});

			// Click Delete Key button
			const deleteButton = within(dialog).getByRole("button", {
				name: "Delete Key",
			});
			await user.click(deleteButton);

			// Should show confirmation
			expect(within(dialog).getByText("Are you sure?")).toBeInTheDocument();

			// Cancel
			const cancelButton = within(dialog).getByRole("button", {
				name: "Cancel",
			});
			await user.click(cancelButton);

			// Confirmation should disappear, Delete Key button should be back
			expect(
				within(dialog).queryByText("Are you sure?"),
			).not.toBeInTheDocument();
			expect(
				within(dialog).getByRole("button", { name: "Delete Key" }),
			).toBeInTheDocument();
		});
	});
});

describe("KeyDetailModal edit validation", () => {
	it("does not call update when name is empty", async () => {
		let putCalled = false;
		server.use(
			http.get("/api/virtual-keys", () => HttpResponse.json([mockVirtualKey])),
			http.put("/api/virtual-keys/vk-001", () => {
				putCalled = true;
				return HttpResponse.json(mockVirtualKey);
			}),
		);

		const { user } = renderWithProviders(<VirtualKeys />);

		await waitFor(() => {
			expect(screen.getByText("Test API Key")).toBeInTheDocument();
		});

		// Open detail modal
		await user.click(screen.getByText("Test API Key"));
		await waitFor(() => {
			expect(
				screen.getByRole("dialog", { name: "Virtual Key Details" }),
			).toBeInTheDocument();
		});

		const dialog = screen.getByRole("dialog", {
			name: "Virtual Key Details",
		});

		// Enter edit mode
		await user.click(within(dialog).getByRole("button", { name: "Edit" }));

		// Clear name (make it empty)
		const nameInput = within(dialog).getByLabelText("Name");
		await user.clear(nameInput);

		// Click Save Changes
		const saveButton = within(dialog).getByRole("button", {
			name: "Save Changes",
		});
		await user.click(saveButton);

		// PUT should not have been called (empty name validation prevents submission)
		expect(putCalled).toBe(false);
	});
});

describe("KeyDetailModal unsaved-changes guard", () => {
	it("prompts when closing with unsaved changes and stays open on cancel", async () => {
		server.use(
			http.get("/api/virtual-keys", () => HttpResponse.json([mockVirtualKey])),
		);

		const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(false);

		const { user } = renderWithProviders(<VirtualKeys />);

		await waitFor(() => {
			expect(screen.getByText("Test API Key")).toBeInTheDocument();
		});

		// Open detail modal
		await user.click(screen.getByText("Test API Key"));
		await waitFor(() => {
			expect(
				screen.getByRole("dialog", { name: "Virtual Key Details" }),
			).toBeInTheDocument();
		});

		const dialog = screen.getByRole("dialog", {
			name: "Virtual Key Details",
		});

		// Enter edit mode and make a change
		await user.click(within(dialog).getByRole("button", { name: "Edit" }));
		const nameInput = within(dialog).getByLabelText("Name");
		await user.clear(nameInput);
		await user.type(nameInput, "Modified Name");

		// Click close (X button)
		await user.click(within(dialog).getByRole("button", { name: "Close" }));

		// Confirm should have been called
		expect(confirmSpy).toHaveBeenCalledWith("Discard unsaved changes?");

		// Modal should still be open (user cancelled confirm)
		expect(
			screen.getByRole("dialog", { name: "Virtual Key Details" }),
		).toBeInTheDocument();

		confirmSpy.mockRestore();
	});

	it("closes modal when confirming discard", async () => {
		server.use(
			http.get("/api/virtual-keys", () => HttpResponse.json([mockVirtualKey])),
		);

		const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);

		const { user } = renderWithProviders(<VirtualKeys />);

		await waitFor(() => {
			expect(screen.getByText("Test API Key")).toBeInTheDocument();
		});

		// Open detail modal
		await user.click(screen.getByText("Test API Key"));
		await waitFor(() => {
			expect(
				screen.getByRole("dialog", { name: "Virtual Key Details" }),
			).toBeInTheDocument();
		});

		const dialog = screen.getByRole("dialog", {
			name: "Virtual Key Details",
		});

		// Enter edit mode and make a change
		await user.click(within(dialog).getByRole("button", { name: "Edit" }));
		const nameInput = within(dialog).getByLabelText("Name");
		await user.clear(nameInput);
		await user.type(nameInput, "Modified Name");

		// Click close (X button)
		await user.click(within(dialog).getByRole("button", { name: "Close" }));

		// Confirm should have been called
		expect(confirmSpy).toHaveBeenCalledWith("Discard unsaved changes?");

		// Modal should be closed (user confirmed discard)
		await waitFor(() => {
			expect(
				screen.queryByRole("dialog", { name: "Virtual Key Details" }),
			).not.toBeInTheDocument();
		});

		confirmSpy.mockRestore();
	});
});

describe("hasChanges revert", () => {
	it("disables Save after editing name back to original", async () => {
		server.use(
			http.get("/api/virtual-keys", () => HttpResponse.json([mockVirtualKey])),
		);

		const { user } = renderWithProviders(<VirtualKeys />);

		await waitFor(() => {
			expect(screen.getByText("Test API Key")).toBeInTheDocument();
		});

		// Open detail modal
		await user.click(screen.getByText("Test API Key"));
		await waitFor(() => {
			expect(
				screen.getByRole("dialog", { name: "Virtual Key Details" }),
			).toBeInTheDocument();
		});

		const dialog = screen.getByRole("dialog", {
			name: "Virtual Key Details",
		});

		// Enter edit mode
		await user.click(within(dialog).getByRole("button", { name: "Edit" }));

		// Edit name to something new
		const nameInput = within(dialog).getByLabelText("Name");
		await user.clear(nameInput);
		await user.type(nameInput, "Changed Name");

		// Save should be enabled
		const saveButton = within(dialog).getByRole("button", {
			name: "Save Changes",
		});
		expect(saveButton).not.toBeDisabled();

		// Revert name back to original
		await user.clear(nameInput);
		await user.type(nameInput, "Test API Key");

		// Save should be disabled again (no changes)
		expect(saveButton).toBeDisabled();
	});
});
