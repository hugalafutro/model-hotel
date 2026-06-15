import { screen, waitFor, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
	mockProvider,
	mockProvider2,
	mockVirtualKey,
	mockVirtualKeyWithProviders,
} from "../../../test/mocks/data";
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
			expect(within(dialog).getByText(mockVirtualKey.id)).toBeInTheDocument();
			expect(
				within(dialog).getByText(mockVirtualKey.key_preview),
			).toBeInTheDocument();
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

		it("shows provider access section in view mode", async () => {
			server.use(
				http.get("/api/providers", () => HttpResponse.json([mockProvider])),
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

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

			// Check Provider Access section header exists
			expect(
				within(dialog).getByText("Provider Access", { exact: false }),
			).toBeInTheDocument();

			// Provider name appears as a tag chip
			expect(within(dialog).getByText("Test Provider")).toBeInTheDocument();
		});

		it("toggles provider selection in edit mode", async () => {
			const mockProviders = [
				mockProvider,
				{
					...mockProvider,
					id: "provider-002",
					name: "Another Provider",
					created_at: "2026-02-20T10:00:00Z",
					updated_at: "2026-05-11T12:00:00Z",
				},
			];

			server.use(
				http.get("/api/providers", () => HttpResponse.json(mockProviders)),
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

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

			// Find provider button - should have aria-pressed="false" initially (not excluded)
			const providerButton = within(dialog).getByRole("button", {
				name: "Test Provider",
			});
			expect(providerButton).toHaveAttribute("aria-pressed", "false");

			// Click provider tag to exclude it
			await user.click(providerButton);

			// Should now be excluded (aria-pressed="true")
			expect(providerButton).toHaveAttribute("aria-pressed", "true");
			// Button should have the excluded styling class
			expect(providerButton.className).toContain("line-through");
		});

		it("resets excluded providers on cancel", async () => {
			server.use(
				http.get("/api/providers", () => HttpResponse.json([mockProvider])),
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

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

			// Exclude a provider
			const providerButton = within(dialog).getByRole("button", {
				name: "Test Provider",
			});
			await user.click(providerButton);
			expect(providerButton).toHaveAttribute("aria-pressed", "true");

			// Cancel edit
			await user.click(within(dialog).getByRole("button", { name: "Cancel" }));

			// Re-enter edit mode
			await user.click(within(dialog).getByRole("button", { name: "Edit" }));

			// Verify provider is no longer excluded (state was reset on cancel)
			const providerButtonAfter = within(dialog).getByRole("button", {
				name: "Test Provider",
			});
			expect(providerButtonAfter).toHaveAttribute("aria-pressed", "false");
		});

		it("initializes excluded providers from allowed_providers on edit", async () => {
			const mockProviders = [
				mockProvider,
				{
					...mockProvider,
					id: "provider-002",
					name: "Other Provider",
					created_at: "2026-02-20T10:00:00Z",
					updated_at: "2026-05-11T12:00:00Z",
				},
			];

			server.use(
				http.get("/api/providers", () => HttpResponse.json(mockProviders)),
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKeyWithProviders]),
				),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Restricted Key")).toBeInTheDocument();
			});

			const nameCell = screen.getByText("Restricted Key");
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

			// provider-001 is in allowed_providers, so it should NOT be excluded
			// aria-pressed="false" means it's allowed (not excluded)
			const allowedProviderButton = within(dialog).getByRole("button", {
				name: "Test Provider",
			});
			expect(allowedProviderButton).toHaveAttribute("aria-pressed", "false");

			// provider-002 is NOT in allowed_providers, so it should be excluded
			// aria-pressed="true" means it's excluded
			const excludedProviderButton = within(dialog).getByRole("button", {
				name: "Other Provider",
			});
			expect(excludedProviderButton).toHaveAttribute("aria-pressed", "true");
		});

		it("sends allowed_providers on save", async () => {
			const mockProviders = [
				mockProvider,
				{
					...mockProvider,
					id: "provider-002",
					name: "Other Provider",
					created_at: "2026-02-20T10:00:00Z",
					updated_at: "2026-05-11T12:00:00Z",
				},
			];

			let updateBody: unknown;
			server.use(
				http.get("/api/providers", () => HttpResponse.json(mockProviders)),
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
				http.put("/api/virtual-keys/vk-001", async ({ request }) => {
					updateBody = await request.json();
					return HttpResponse.json({
						...mockVirtualKey,
						name: (updateBody as { name: string }).name,
					});
				}),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

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

			// Initially no providers are excluded (aria-pressed="false" for all)
			const providerButton1 = within(dialog).getByRole("button", {
				name: "Test Provider",
			});
			expect(providerButton1).toHaveAttribute("aria-pressed", "false");

			// Exclude provider-002
			const providerButton2 = within(dialog).getByRole("button", {
				name: "Other Provider",
			});
			await user.click(providerButton2);
			expect(providerButton2).toHaveAttribute("aria-pressed", "true");

			// Click Save Changes
			const saveButton = within(dialog).getByRole("button", {
				name: "Save Changes",
			});
			await user.click(saveButton);

			await waitFor(() => {
				expect(screen.getByText("Virtual key updated")).toBeInTheDocument();
			});

			// Verify the update call was made with correct allowed_providers
			// After excluding provider-002, only provider-001 should be allowed
			expect(updateBody).toBeDefined();
			const body = updateBody as { allowed_providers?: string[] };
			expect(body.allowed_providers).toEqual(["provider-001"]);
		});

		it("shows reset button when providers are excluded", async () => {
			server.use(
				http.get("/api/providers", () => HttpResponse.json([mockProvider])),
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

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

			// No reset button initially
			expect(
				within(dialog).queryByLabelText("Restore access to all providers"),
			).not.toBeInTheDocument();

			// Exclude a provider
			const providerButton = within(dialog).getByRole("button", {
				name: "Test Provider",
			});
			await user.click(providerButton);

			// Reset button should appear
			const resetButton = within(dialog).getByLabelText(
				"Restore access to all providers",
			);
			expect(resetButton).toBeInTheDocument();

			// Click reset
			await user.click(resetButton);

			// Provider should be restored (not excluded)
			expect(providerButton).toHaveAttribute("aria-pressed", "false");
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

			// Update RPS (select by stable id, not translated label text)
			const rateLimitRpsInput =
				dialog.querySelector<HTMLInputElement>("#vk-detail-rps");
			if (!rateLimitRpsInput) throw new Error("rps input not found");
			await user.clear(rateLimitRpsInput);
			await user.type(rateLimitRpsInput, "100");

			// Update BURST
			const rateLimitBurstInput =
				dialog.querySelector<HTMLInputElement>("#vk-detail-burst");
			if (!rateLimitBurstInput) throw new Error("burst input not found");
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

		it("edits and sends rate_limit_tpm on save", async () => {
			let updateBody: unknown;
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
				http.put("/api/virtual-keys/vk-001", async ({ request }) => {
					updateBody = await request.json();
					return HttpResponse.json({
						...mockVirtualKey,
						rate_limit_tpm: (updateBody as { rate_limit_tpm: number | null })
							.rate_limit_tpm,
					});
				}),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

			await user.click(screen.getByText("Test API Key"));

			const dialog = await screen.findByRole("dialog", {
				name: "Virtual Key Details",
			});

			await user.click(within(dialog).getByRole("button", { name: "Edit" }));

			const tpmInput = dialog.querySelector<HTMLInputElement>("#vk-detail-tpm");
			if (!tpmInput) throw new Error("tpm input not found");
			await user.clear(tpmInput);
			await user.type(tpmInput, "30000");

			await user.click(
				within(dialog).getByRole("button", { name: "Save Changes" }),
			);

			await waitFor(() => {
				expect(screen.getByText("Virtual key updated")).toBeInTheDocument();
			});

			expect(updateBody).toBeDefined();
			expect(
				(updateBody as { rate_limit_tpm?: number | null }).rate_limit_tpm,
			).toBe(30000);
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

		it("toggles strip reasoning in edit mode", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

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

			// Find the strip reasoning toggle (Toggle component uses role="switch")
			const toggle = within(dialog).getByRole("switch", {
				name: "Strip Reasoning",
			});
			expect(toggle).toHaveAttribute("aria-checked", "false");

			// Click the toggle
			await user.click(toggle);

			// Should now be enabled
			expect(toggle).toHaveAttribute("aria-checked", "true");
			expect(within(dialog).getByText("Enabled")).toBeInTheDocument();
		});

		it("renders providers in alphabetical order in edit mode", async () => {
			const zetaProvider = {
				...mockProvider,
				id: "p-zeta",
				name: "Zeta Provider",
				created_at: "2026-02-20T10:00:00Z",
				updated_at: "2026-05-11T12:00:00Z",
			};
			const alphaProvider = {
				...mockProvider,
				id: "p-alpha",
				name: "Alpha Provider",
				created_at: "2026-02-20T10:00:00Z",
				updated_at: "2026-05-11T12:00:00Z",
			};

			server.use(
				http.get("/api/providers", () =>
					HttpResponse.json([zetaProvider, alphaProvider]),
				),
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

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

			// Get all provider buttons (they have aria-pressed attribute)
			const providerButtons = within(dialog)
				.getAllByRole("button")
				.filter(
					(btn) =>
						btn.getAttribute("aria-pressed") !== null &&
						btn.textContent?.trim(),
				);
			const names = providerButtons.map((btn) => btn.textContent);
			expect(names).toEqual(["Alpha Provider", "Zeta Provider"]);
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

		// Confirm should have been called (after fade animation)
		await waitFor(() => {
			expect(confirmSpy).toHaveBeenCalledWith("Discard unsaved changes?");
		});

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

		// Confirm should have been called (after fade animation)
		await waitFor(() => {
			expect(confirmSpy).toHaveBeenCalledWith("Discard unsaved changes?");
		});

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

	it("disables Save for unchanged provider exclusions", async () => {
		const mockProviders = [
			mockProvider,
			{
				...mockProvider,
				id: "provider-002",
				name: "Other Provider",
				created_at: "2026-02-20T10:00:00Z",
				updated_at: "2026-05-11T12:00:00Z",
			},
		];

		server.use(
			http.get("/api/providers", () => HttpResponse.json(mockProviders)),
			http.get("/api/virtual-keys", () =>
				HttpResponse.json([mockVirtualKeyWithProviders]),
			),
		);

		const { user } = renderWithProviders(<VirtualKeys />);

		await waitFor(() => {
			expect(screen.getByText("Restricted Key")).toBeInTheDocument();
		});

		const nameCell = screen.getByText("Restricted Key");
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

		// Save button should be disabled (no changes to provider exclusions)
		const saveButton = within(dialog).getByRole("button", {
			name: "Save Changes",
		});
		expect(saveButton).toBeDisabled();
	});

	it("blocks edit when key has restrictions and providers not loaded", async () => {
		server.use(
			// Providers never resolve — simulates in-flight query
			http.get("/api/providers", () => new Promise(() => {})),
			http.get("/api/virtual-keys", () =>
				HttpResponse.json([mockVirtualKeyWithProviders]),
			),
		);

		const { user } = renderWithProviders(<VirtualKeys />);

		await waitFor(() => {
			expect(screen.getByText("Restricted Key")).toBeInTheDocument();
		});

		const nameCell = screen.getByText("Restricted Key");
		await user.click(nameCell);

		await waitFor(() => {
			expect(
				screen.getByRole("dialog", { name: "Virtual Key Details" }),
			).toBeInTheDocument();
		});

		const dialog = screen.getByRole("dialog", {
			name: "Virtual Key Details",
		});

		// Click Edit — should NOT enter edit mode because providers haven't loaded
		const editButton = within(dialog).getByRole("button", {
			name: "Edit",
		});
		await user.click(editButton);

		// Should still be in view mode (no input fields)
		expect(within(dialog).queryByLabelText("Name")).not.toBeInTheDocument();
	});

	it("shows error when all providers are excluded on save", async () => {
		const mockProviders = [mockProvider, mockProvider2];

		server.use(
			http.get("/api/providers", () => HttpResponse.json(mockProviders)),
			http.get("/api/virtual-keys", () => HttpResponse.json([mockVirtualKey])),
		);

		const { user } = renderWithProviders(<VirtualKeys />);

		await waitFor(() => {
			expect(screen.getByText("Test API Key")).toBeInTheDocument();
		});

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

		const editButton = within(dialog).getByRole("button", {
			name: "Edit",
		});
		await user.click(editButton);

		// Exclude all providers
		for (const provider of mockProviders) {
			const chip = within(dialog).getByRole("button", {
				name: provider.name,
			});
			await user.click(chip);
		}

		const saveButton = within(dialog).getByRole("button", {
			name: "Save Changes",
		});
		await user.click(saveButton);

		// Should show error message, not call the API
		await waitFor(() => {
			expect(
				screen.getByText("At least one provider must remain accessible"),
			).toBeInTheDocument();
		});
	});
});
