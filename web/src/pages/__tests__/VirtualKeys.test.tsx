import { screen, waitFor, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { VirtualKey } from "../../api/types";
import { getByDialogName, mockVirtualKeys } from "../../test/helpers";
import { mockVirtualKey } from "../../test/mocks/data";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { VirtualKeys } from "../VirtualKeys";

describe("VirtualKeys", () => {
	beforeEach(() => {
		server.resetHandlers();
	});

	describe("Rendering", () => {
		it("renders loading spinner initially", () => {
			// Override handler to delay response
			server.use(
				http.get("/api/virtual-keys", () => {
					return new Promise((resolve) => {
						setTimeout(() => {
							resolve(HttpResponse.json([mockVirtualKey]));
						}, 100);
					});
				}),
			);

			renderWithProviders(<VirtualKeys />);
			expect(screen.getByTestId("spinner")).toBeInTheDocument();
		});

		it("renders empty state when no virtual keys", async () => {
			server.use(...mockVirtualKeys({ body: [] }));

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(
					screen.getByText(
						"No virtual keys. Create one to start using the proxy.",
					),
				).toBeInTheDocument();
			});
		});

		it("renders virtual keys list with data", async () => {
			const mockKeys = [
				{
					...mockVirtualKey,
					id: "vk-001",
					name: "Production Key",
					key_preview: "sk_prod_••••",
					tokens_used: 100000,
					created_at: "2026-05-01T10:00:00Z",
					last_used_at: "2026-05-11T08:00:00Z",
				},
				{
					...mockVirtualKey,
					id: "vk-002",
					name: "Development Key",
					key_preview: "sk_dev_••••",
					tokens_used: 50000,
					created_at: "2026-05-05T12:00:00Z",
					last_used_at: "2026-05-10T15:30:00Z",
				},
			];

			server.use(
				http.get("/api/virtual-keys", () => {
					return HttpResponse.json(mockKeys);
				}),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Production Key")).toBeInTheDocument();
				expect(screen.getByText("Development Key")).toBeInTheDocument();
				expect(screen.getByText("sk_prod_••••")).toBeInTheDocument();
				expect(screen.getByText("sk_dev_••••")).toBeInTheDocument();
			});
		});

		it("renders page header with correct title and count", async () => {
			const mockKeys = [mockVirtualKey, { ...mockVirtualKey, id: "vk-002" }];

			server.use(
				http.get("/api/virtual-keys", () => {
					return HttpResponse.json(mockKeys);
				}),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("2 Virtual Keys")).toBeInTheDocument();
			});
		});

		it("renders proxy URL as copyable pill in header subtitle", async () => {
			server.use(...mockVirtualKeys());

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				const copyBtn = screen.getByRole("button", {
					name: "Click to copy proxy URL",
				});
				expect(copyBtn).toBeInTheDocument();
				expect(copyBtn.querySelector("span")).toHaveTextContent(
					/^http:\/\/localhost:\d+\/v1$/,
				);
			});
		});

		it("renders create key button", async () => {
			server.use(...mockVirtualKeys());

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "+ Create Key" }),
				).toBeInTheDocument();
			});
		});
	});

	describe("Create Key Modal", () => {
		it("opens create modal when clicking Create Key button", async () => {
			server.use(...mockVirtualKeys());

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "+ Create Key" }),
				).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: "+ Create Key" }));

			expect(getByDialogName("Create Virtual Key")).toBeInTheDocument();
			expect(screen.getByLabelText("Name")).toBeInTheDocument();
			expect(
				screen.getByLabelText("Rate Limit RPS (requests/sec)"),
			).toBeInTheDocument();
			expect(
				screen.getByLabelText("Rate Limit Burst (max concurrent)"),
			).toBeInTheDocument();
		});

		it("creates a new virtual key with name only", async () => {
			server.use(
				...mockVirtualKeys(),
				http.post("/api/virtual-keys", async ({ request }) => {
					const body = await request.json();
					const newKey: VirtualKey = {
						...mockVirtualKey,
						id: `vk-${Date.now()}`,
						name: (body as { name?: string }).name || "New Key",
						key_preview: "sk_test_••••",
						created_at: new Date().toISOString(),
					};
					return HttpResponse.json(newKey, { status: 201 });
				}),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "+ Create Key" }),
				).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: "+ Create Key" }));

			await user.type(screen.getByLabelText("Name"), "My New Key");

			await user.click(screen.getByRole("button", { name: "Create Key" }));

			await waitFor(() => {
				expect(screen.getByText("Virtual Key Created")).toBeInTheDocument();
			});

			// Key should be displayed with copy functionality
			await waitFor(() => {
				expect(screen.getByText(/sk_test_/)).toBeInTheDocument();
			});
		});

		it("creates a new virtual key with rate limits", async () => {
			server.use(...mockVirtualKeys());

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "+ Create Key" }),
				).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: "+ Create Key" }));

			await user.type(screen.getByLabelText("Name"), "Rate Limited Key");
			await user.type(
				screen.getByLabelText("Rate Limit RPS (requests/sec)"),
				"50",
			);
			await user.type(
				screen.getByLabelText("Rate Limit Burst (max concurrent)"),
				"100",
			);

			await user.click(screen.getByRole("button", { name: "Create Key" }));

			await waitFor(() => {
				expect(screen.getByText("Virtual Key Created")).toBeInTheDocument();
			});
		});

		it("shows error toast when creation fails", async () => {
			server.use(
				...mockVirtualKeys(),
				http.post("/api/virtual-keys", async ({ request }) => {
					if (!request.headers.get("Authorization")) {
						return HttpResponse.json(
							{ error: "Unauthorized" },
							{ status: 401 },
						);
					}
					return HttpResponse.json(
						{ error: "Name already exists" },
						{ status: 400 },
					);
				}),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "+ Create Key" }),
				).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: "+ Create Key" }));

			await user.type(screen.getByLabelText("Name"), "Duplicate Key");

			await user.click(screen.getByRole("button", { name: "Create Key" }));

			// Toast appears with error message
			await waitFor(() => {
				const toast = screen.getByRole("button", { name: /Failed:/ });
				expect(toast).toBeInTheDocument();
				expect(toast.textContent).toContain("Name already exists");
			});
		});

		it("shows key only once after creation with copy functionality", async () => {
			server.use(...mockVirtualKeys());

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "+ Create Key" }),
				).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: "+ Create Key" }));

			await user.type(screen.getByLabelText("Name"), "One Time Key");

			await user.click(screen.getByRole("button", { name: "Create Key" }));

			await waitFor(() => {
				expect(screen.getByText("Virtual Key Created")).toBeInTheDocument();
			});

			expect(
				screen.getByText("Copy this key now. It won't be shown again."),
			).toBeInTheDocument();
			expect(screen.getByText(/sk_test_/)).toBeInTheDocument();
		});

		it("closes modal after clicking Done button", async () => {
			server.use(...mockVirtualKeys());

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "+ Create Key" }),
				).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: "+ Create Key" }));

			await user.type(screen.getByLabelText("Name"), "Test Key");

			await user.click(screen.getByRole("button", { name: "Create Key" }));

			await waitFor(() => {
				expect(screen.getByText("Virtual Key Created")).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: "Done" }));

			await waitFor(() => {
				expect(
					screen.queryByText("Virtual Key Created"),
				).not.toBeInTheDocument();
			});
		});

		it("closes modal when clicking Cancel button", async () => {
			server.use(...mockVirtualKeys());

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "+ Create Key" }),
				).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: "+ Create Key" }));

			expect(getByDialogName("Create Virtual Key")).toBeInTheDocument();

			await user.click(screen.getByRole("button", { name: "Cancel" }));

			await waitFor(() => {
				expect(
					screen.queryByRole("dialog", { name: "Create Virtual Key" }),
				).not.toBeInTheDocument();
			});
		});

		it("validates name field is required", async () => {
			server.use(
				http.get("/api/virtual-keys", () => {
					return HttpResponse.json([mockVirtualKey]);
				}),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "+ Create Key" }),
				).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: "+ Create Key" }));

			// Try to submit without name
			await user.click(screen.getByRole("button", { name: "Create Key" }));

			// Form should not submit (name is required with HTML5 validation)
			await waitFor(() => {
				expect(
					screen.queryByText("Virtual Key Created"),
				).not.toBeInTheDocument();
			});
		});
	});

	describe("Edit Key Modal", () => {
		it("opens edit mode from detail modal", async () => {
			server.use(...mockVirtualKeys());

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

			// Click the row to open detail modal
			const table = screen.getByRole("table");
			const row = table.querySelector("tbody tr");
			expect(row).toBeInTheDocument();
			await user.click(row as HTMLElement);

			// Wait for detail modal and click Edit button
			await waitFor(() => {
				expect(getByDialogName("Virtual Key Details")).toBeInTheDocument();
			});
			await user.click(screen.getByRole("button", { name: "Edit" }));

			// Should be in edit mode with Name input
			expect(screen.getByLabelText("Name")).toHaveValue("Test API Key");
		});

		it("updates virtual key name", async () => {
			server.use(
				...mockVirtualKeys(),
				http.put("/api/virtual-keys/:id", async ({ params }) => {
					return HttpResponse.json({
						...mockVirtualKey,
						id: params.id as string,
						name: "Updated Key Name",
					});
				}),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

			// Click row to open detail modal
			const table = screen.getByRole("table");
			const row = table.querySelector("tbody tr");
			await user.click(row as HTMLElement);

			// Wait for detail modal and click Edit
			await waitFor(() => {
				expect(getByDialogName("Virtual Key Details")).toBeInTheDocument();
			});
			await user.click(screen.getByRole("button", { name: "Edit" }));

			const nameInput = screen.getByLabelText("Name");
			await user.clear(nameInput);
			await user.type(nameInput, "Updated Key Name");

			await user.click(screen.getByRole("button", { name: "Save Changes" }));

			await waitFor(() => {
				expect(screen.getByText("Virtual key updated")).toBeInTheDocument();
			});
		});

		it("updates rate limits", async () => {
			server.use(
				...mockVirtualKeys(),
				http.put("/api/virtual-keys/:id", async ({ request, params }) => {
					const body = (await request.json()) as Partial<typeof mockVirtualKey>;
					return HttpResponse.json({
						...mockVirtualKey,
						id: params.id as string,
						...body,
					});
				}),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

			// Click row to open detail modal
			const table = screen.getByRole("table");
			const row = table.querySelector("tbody tr");
			await user.click(row as HTMLElement);

			// Wait for detail modal and click Edit
			await waitFor(() => {
				expect(getByDialogName("Virtual Key Details")).toBeInTheDocument();
			});
			await user.click(screen.getByRole("button", { name: "Edit" }));

			await user.clear(screen.getByLabelText("Rate Limit RPS (requests/sec)"));
			await user.type(
				screen.getByLabelText("Rate Limit RPS (requests/sec)"),
				"100",
			);

			await user.clear(
				screen.getByLabelText("Rate Limit Burst (max concurrent)"),
			);
			await user.type(
				screen.getByLabelText("Rate Limit Burst (max concurrent)"),
				"200",
			);

			await user.click(screen.getByRole("button", { name: "Save Changes" }));

			await waitFor(() => {
				expect(screen.getByText("Virtual key updated")).toBeInTheDocument();
			});
		});

		it("shows error toast when update fails", async () => {
			server.use(
				...mockVirtualKeys(),
				http.put("/api/virtual-keys/:id", async ({ request }) => {
					if (!request.headers.get("Authorization")) {
						return HttpResponse.json(
							{ error: "Unauthorized" },
							{ status: 401 },
						);
					}
					return HttpResponse.json({ error: "Update failed" }, { status: 400 });
				}),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

			// Click row to open detail modal
			const table = screen.getByRole("table");
			const row = table.querySelector("tbody tr");
			await user.click(row as HTMLElement);

			// Wait for detail modal and click Edit
			await waitFor(() => {
				expect(getByDialogName("Virtual Key Details")).toBeInTheDocument();
			});
			await user.click(screen.getByRole("button", { name: "Edit" }));

			await user.type(screen.getByLabelText("Name"), " Updated");

			await user.click(screen.getByRole("button", { name: "Save Changes" }));

			// Toast appears with error message
			await waitFor(() => {
				const toast = screen.getByRole("button", { name: /Failed:/ });
				expect(toast).toBeInTheDocument();
				expect(toast.textContent).toContain("Update failed");
			});
		});

		it("closes edit modal on successful update", async () => {
			server.use(
				...mockVirtualKeys(),
				http.put("/api/virtual-keys/:id", () => {
					return HttpResponse.json(mockVirtualKey);
				}),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

			// Click row to open detail modal
			const table = screen.getByRole("table");
			const row = table.querySelector("tbody tr");
			await user.click(row as HTMLElement);

			// Wait for detail modal and click Edit
			await waitFor(() => {
				expect(getByDialogName("Virtual Key Details")).toBeInTheDocument();
			});
			await user.click(screen.getByRole("button", { name: "Edit" }));

			// Make a change to enable the Save button
			await user.type(screen.getByLabelText("Name"), " Updated");

			await user.click(screen.getByRole("button", { name: "Save Changes" }));

			// Modal should close on successful update
			await waitFor(() => {
				expect(
					screen.queryByRole("dialog", { name: "Virtual Key Details" }),
				).not.toBeInTheDocument();
			});
		});

		it("closes edit modal when clicking Cancel", async () => {
			server.use(...mockVirtualKeys());

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

			// Click row to open detail modal
			const table = screen.getByRole("table");
			const row = table.querySelector("tbody tr");
			await user.click(row as HTMLElement);

			// Wait for detail modal and click Edit
			await waitFor(() => {
				expect(getByDialogName("Virtual Key Details")).toBeInTheDocument();
			});
			await user.click(screen.getByRole("button", { name: "Edit" }));

			// Click Cancel - should return to view mode (modal stays open)
			await user.click(screen.getByRole("button", { name: "Cancel" }));

			// Should still have modal open but in view mode
			await waitFor(() => {
				expect(getByDialogName("Virtual Key Details")).toBeInTheDocument();
				expect(
					screen.getByRole("button", { name: "Edit" }),
				).toBeInTheDocument();
			});
		});
	});

	describe("KeyDetailModal edit validation", () => {
		it("does not call update when name is empty", async () => {
			let putCalled = false;
			server.use(
				...mockVirtualKeys(),
				http.put("/api/virtual-keys/:id", () => {
					putCalled = true;
					return HttpResponse.json(mockVirtualKey);
				}),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

			const table = screen.getByRole("table");
			const row = table.querySelector("tbody tr");
			await user.click(row as HTMLElement);

			await waitFor(() => {
				expect(getByDialogName("Virtual Key Details")).toBeInTheDocument();
			});
			await user.click(screen.getByRole("button", { name: "Edit" }));

			const nameInput = screen.getByLabelText("Name");
			await user.clear(nameInput);

			await user.click(screen.getByRole("button", { name: "Save Changes" }));

			await waitFor(() => {
				expect(putCalled).toBe(false);
			});
			expect(putCalled).toBe(false);
		});

		it("does not call update when name is whitespace only", async () => {
			let putCalled = false;
			server.use(
				...mockVirtualKeys(),
				http.put("/api/virtual-keys/:id", () => {
					putCalled = true;
					return HttpResponse.json(mockVirtualKey);
				}),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

			const table = screen.getByRole("table");
			const row = table.querySelector("tbody tr");
			await user.click(row as HTMLElement);

			await waitFor(() => {
				expect(getByDialogName("Virtual Key Details")).toBeInTheDocument();
			});
			await user.click(screen.getByRole("button", { name: "Edit" }));

			const nameInput = screen.getByLabelText("Name");
			await user.clear(nameInput);
			await user.type(nameInput, "   ");

			await user.click(screen.getByRole("button", { name: "Save Changes" }));

			await waitFor(() => {
				expect(putCalled).toBe(false);
			});
			expect(putCalled).toBe(false);
		});

		it("sends null for rate_limit_rps when field is cleared", async () => {
			let capturedBody: unknown;
			server.use(
				...mockVirtualKeys(),
				http.put("/api/virtual-keys/:id", async ({ request }) => {
					capturedBody = await request.json();
					return HttpResponse.json(mockVirtualKey);
				}),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

			const table = screen.getByRole("table");
			const row = table.querySelector("tbody tr");
			await user.click(row as HTMLElement);

			await waitFor(() => {
				expect(getByDialogName("Virtual Key Details")).toBeInTheDocument();
			});
			await user.click(screen.getByRole("button", { name: "Edit" }));

			const rpsInput = screen.getByLabelText("Rate Limit RPS (requests/sec)");
			await user.clear(rpsInput);

			await user.click(screen.getByRole("button", { name: "Save Changes" }));

			await waitFor(() => {
				expect(screen.getByText("Virtual key updated")).toBeInTheDocument();
			});

			expect(capturedBody).toEqual({
				name: "Test API Key",
				rate_limit_rps: null,
				rate_limit_burst: 60,
			});
		});

		it("sends null for rate_limit_burst when field is cleared", async () => {
			let capturedBody: unknown;
			server.use(
				...mockVirtualKeys(),
				http.put("/api/virtual-keys/:id", async ({ request }) => {
					capturedBody = await request.json();
					return HttpResponse.json(mockVirtualKey);
				}),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

			const table = screen.getByRole("table");
			const row = table.querySelector("tbody tr");
			await user.click(row as HTMLElement);

			await waitFor(() => {
				expect(getByDialogName("Virtual Key Details")).toBeInTheDocument();
			});
			await user.click(screen.getByRole("button", { name: "Edit" }));

			const burstInput = screen.getByLabelText(
				"Rate Limit Burst (max concurrent)",
			);
			await user.clear(burstInput);

			await user.click(screen.getByRole("button", { name: "Save Changes" }));

			await waitFor(() => {
				expect(screen.getByText("Virtual key updated")).toBeInTheDocument();
			});

			expect(capturedBody).toEqual({
				name: "Test API Key",
				rate_limit_rps: 30,
				rate_limit_burst: null,
			});
		});

		it("Save Changes button is disabled when no changes made", async () => {
			server.use(...mockVirtualKeys());

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

			const table = screen.getByRole("table");
			const row = table.querySelector("tbody tr");
			await user.click(row as HTMLElement);

			await waitFor(() => {
				expect(getByDialogName("Virtual Key Details")).toBeInTheDocument();
			});
			await user.click(screen.getByRole("button", { name: "Edit" }));

			expect(
				screen.getByRole("button", { name: "Save Changes" }),
			).toBeDisabled();
		});
	});

	describe("Key Detail Modal", () => {
		it("opens detail modal when clicking key name", async () => {
			server.use(...mockVirtualKeys());

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

			// Click the row to open detail modal
			const table = screen.getByRole("table");
			const row = table.querySelector("tbody tr");
			expect(row).toBeInTheDocument();
			await user.click(row as HTMLElement);

			await waitFor(() => {
				expect(getByDialogName("Virtual Key Details")).toBeInTheDocument();
			});
			// Key preview appears in modal (in a <p> element with text-gray-200 font-mono)
			const modalKeyPreview = screen.getByText("sk_test_••••", {
				selector: "p.font-mono",
			});
			expect(modalKeyPreview).toBeInTheDocument();
		});

		it("shows key details in modal", async () => {
			server.use(...mockVirtualKeys());

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

			// Click row to open detail modal
			const table = screen.getByRole("table");
			const row = table.querySelector("tbody tr");
			await user.click(row as HTMLElement);

			await waitFor(() => {
				expect(getByDialogName("Virtual Key Details")).toBeInTheDocument();
			});
			// Check modal content - use getAllByText since labels appear in both table and modal
			expect(screen.getAllByText("Name")).toHaveLength(2); // header + modal label
			expect(screen.getAllByText("Key")).toHaveLength(2);
			expect(screen.getAllByText("Created")).toHaveLength(2);
			expect(screen.getByText("Tokens Consumed")).toBeInTheDocument();
			expect(screen.getAllByText("Last Used")).toHaveLength(2);
		});

		it("shows 'Never' for last used when null", async () => {
			const mockKeyWithoutUsage = {
				...mockVirtualKey,
				last_used_at: null,
			};

			server.use(
				http.get("/api/virtual-keys", () => {
					return HttpResponse.json([mockKeyWithoutUsage]);
				}),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

			// Click row to open detail modal
			const table = screen.getByRole("table");
			const row = table.querySelector("tbody tr");
			await user.click(row as HTMLElement);

			await waitFor(() => {
				expect(getByDialogName("Virtual Key Details")).toBeInTheDocument();
			});
			// Check modal shows "Never" for last used - query within modal
			const modal = screen.getByRole("dialog");
			expect(within(modal).getByText("Never")).toBeInTheDocument();
		});

		it("deletes virtual key from detail modal", async () => {
			server.use(
				...mockVirtualKeys(),
				http.delete("/api/virtual-keys/:id", () => {
					return new HttpResponse(null, { status: 204 });
				}),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

			// Click row to open detail modal
			const table = screen.getByRole("table");
			const row = table.querySelector("tbody tr");
			await user.click(row as HTMLElement);

			// Click delete button
			const dialog = getByDialogName("Virtual Key Details");
			await user.click(
				within(dialog).getByRole("button", { name: "Delete Key" }),
			);

			// Confirm deletion
			await user.click(
				within(dialog).getByRole("button", { name: "Yes, delete" }),
			);

			await waitFor(() => {
				expect(screen.getByText("Virtual key deleted")).toBeInTheDocument();
			});

			await waitFor(() => {
				expect(
					screen.queryByRole("dialog", { name: "Virtual Key Details" }),
				).not.toBeInTheDocument();
			});
		});

		it("cancels deletion in confirm state", async () => {
			server.use(...mockVirtualKeys());

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

			// Click row to open detail modal
			const table = screen.getByRole("table");
			const row = table.querySelector("tbody tr");
			await user.click(row as HTMLElement);

			// Click delete button
			const dialog = getByDialogName("Virtual Key Details");
			await user.click(
				within(dialog).getByRole("button", { name: "Delete Key" }),
			);

			expect(screen.getByText("Are you sure?")).toBeInTheDocument();

			// Cancel
			await user.click(within(dialog).getByRole("button", { name: "Cancel" }));

			expect(screen.queryByText("Are you sure?")).not.toBeInTheDocument();
			expect(screen.getByText("Delete Key")).toBeInTheDocument();
		});

		it("shows error toast when deletion fails", async () => {
			server.use(
				...mockVirtualKeys(),
				http.delete("/api/virtual-keys/:id", async ({ request, params }) => {
					if (!request.headers.get("Authorization")) {
						return HttpResponse.json(
							{ error: "Unauthorized" },
							{ status: 401 },
						);
					}
					// Return error for the specific test key
					if (params.id === mockVirtualKey.id) {
						return HttpResponse.json(
							{ error: "Deletion failed" },
							{ status: 500 },
						);
					}
					return new HttpResponse(null, { status: 204 });
				}),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

			// Click row to open detail modal
			const table = screen.getByRole("table");
			const row = table.querySelector("tbody tr");
			await user.click(row as HTMLElement);

			await waitFor(() => {
				expect(getByDialogName("Virtual Key Details")).toBeInTheDocument();
			});

			const dialog = getByDialogName("Virtual Key Details");
			await user.click(
				within(dialog).getByRole("button", { name: "Delete Key" }),
			);
			await user.click(
				within(dialog).getByRole("button", { name: "Yes, delete" }),
			);

			// Toast appears with error message
			await waitFor(() => {
				const toast = screen.getByRole("button", { name: /Failed to delete:/ });
				expect(toast).toBeInTheDocument();
			});
		});

		it("closes detail modal when clicking close button", async () => {
			server.use(...mockVirtualKeys());

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

			// Click row to open detail modal
			const table = screen.getByRole("table");
			const row = table.querySelector("tbody tr");
			await user.click(row as HTMLElement);

			expect(getByDialogName("Virtual Key Details")).toBeInTheDocument();

			const closeButton = screen.getByRole("button", { name: "Close" });
			await user.click(closeButton);

			await waitFor(() => {
				expect(
					screen.queryByRole("dialog", { name: "Virtual Key Details" }),
				).not.toBeInTheDocument();
			});
		});
	});

	describe("Sorting", () => {
		it("sorts by name ascending", async () => {
			const mockKeys = [
				{ ...mockVirtualKey, id: "vk-001", name: "Zebra Key" },
				{ ...mockVirtualKey, id: "vk-002", name: "Alpha Key" },
				{ ...mockVirtualKey, id: "vk-003", name: "Beta Key" },
			];

			server.use(
				http.get("/api/virtual-keys", () => {
					return HttpResponse.json(mockKeys);
				}),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Zebra Key")).toBeInTheDocument();
			});

			// Initial sort is name ascending, so data is already sorted
			// Click twice to get back to ascending (first click -> desc, second -> asc)
			await user.click(screen.getByRole("button", { name: /Sort by Name/i }));
			await user.click(screen.getByRole("button", { name: /Sort by Name/i }));

			// Should be sorted ascending: Alpha, Beta, Zebra
			// Get all key names in the table body
			const table = screen.getByRole("table");
			const rows = table.querySelectorAll("tbody tr");
			expect(rows).toHaveLength(3);
			// First should be Alpha, second Beta, third Zebra
			expect(rows[0].querySelector("td")?.textContent).toBe("Alpha Key");
			expect(rows[1].querySelector("td")?.textContent).toBe("Beta Key");
			expect(rows[2].querySelector("td")?.textContent).toBe("Zebra Key");
		});

		it("sorts by name descending", async () => {
			const mockKeys = [
				{ ...mockVirtualKey, id: "vk-001", name: "Alpha Key" },
				{ ...mockVirtualKey, id: "vk-002", name: "Beta Key" },
			];

			server.use(
				http.get("/api/virtual-keys", () => {
					return HttpResponse.json(mockKeys);
				}),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Alpha Key")).toBeInTheDocument();
			});

			// Default sort is name ascending, click once for descending
			await user.click(screen.getByRole("button", { name: /Sort by Name/i }));

			// Should be sorted descending: Beta, Alpha
			const table = screen.getByRole("table");
			const rows = table.querySelectorAll("tbody tr");
			expect(rows).toHaveLength(2);
			// First should be Beta, second Alpha (descending)
			expect(rows[0].querySelector("td")?.textContent).toBe("Beta Key");
			expect(rows[1].querySelector("td")?.textContent).toBe("Alpha Key");
		});

		it("sorts by created date", async () => {
			const mockKeys = [
				{
					...mockVirtualKey,
					id: "vk-001",
					name: "New Key",
					created_at: "2026-05-10T10:00:00Z",
				},
				{
					...mockVirtualKey,
					id: "vk-002",
					name: "Old Key",
					created_at: "2026-01-01T10:00:00Z",
				},
			];

			server.use(
				http.get("/api/virtual-keys", () => {
					return HttpResponse.json(mockKeys);
				}),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("New Key")).toBeInTheDocument();
			});

			await user.click(
				screen.getByRole("button", { name: /Sort by Created/i }),
			);

			// Old key should be first (ascending by date)
			const table = screen.getByRole("table");
			const rows = table.querySelectorAll("tbody tr");
			expect(rows[0].querySelector("td")?.textContent).toBe("Old Key");
		});

		it("sorts by tokens used", async () => {
			const mockKeys = [
				{
					...mockVirtualKey,
					id: "vk-001",
					name: "High Usage",
					tokens_used: 1000000,
				},
				{
					...mockVirtualKey,
					id: "vk-002",
					name: "Low Usage",
					tokens_used: 1000,
				},
			];

			server.use(
				http.get("/api/virtual-keys", () => {
					return HttpResponse.json(mockKeys);
				}),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("High Usage")).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: /Sort by Tokens/i }));

			const table = screen.getByRole("table");
			const rows = table.querySelectorAll("tbody tr");
			expect(rows[0].querySelector("td")?.textContent).toBe("Low Usage");
		});

		it("sorts by last used", async () => {
			const mockKeys = [
				{
					...mockVirtualKey,
					id: "vk-001",
					name: "Recent",
					last_used_at: "2026-05-11T10:00:00Z",
				},
				{
					...mockVirtualKey,
					id: "vk-002",
					name: "Old",
					last_used_at: "2026-05-01T10:00:00Z",
				},
			];

			server.use(
				http.get("/api/virtual-keys", () => {
					return HttpResponse.json(mockKeys);
				}),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Recent")).toBeInTheDocument();
			});

			await user.click(
				screen.getByRole("button", { name: /Sort by Last Used/i }),
			);

			const table = screen.getByRole("table");
			const rows = table.querySelectorAll("tbody tr");
			expect(rows[0].querySelector("td")?.textContent).toBe("Old");
		});
	});

	describe("Pagination", () => {
		it("renders pagination bar with multiple keys", async () => {
			const mockKeys = Array.from({ length: 15 }, (_, i) => ({
				...mockVirtualKey,
				id: `vk-${i}`,
				name: `Key ${i + 1}`,
			}));

			server.use(
				http.get("/api/virtual-keys", () => {
					return HttpResponse.json(mockKeys);
				}),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Key 1")).toBeInTheDocument();
			});

			expect(screen.getByRole("button", { name: "Prev" })).toBeInTheDocument();
			expect(screen.getByRole("button", { name: "Next" })).toBeInTheDocument();
		});

		it("shows correct page size selector", async () => {
			const mockKeys = Array.from({ length: 25 }, (_, i) => ({
				...mockVirtualKey,
				id: `vk-${i}`,
				name: `Key ${i + 1}`,
			}));

			server.use(
				http.get("/api/virtual-keys", () => {
					return HttpResponse.json(mockKeys);
				}),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Key 1")).toBeInTheDocument();
			});

			const selector = screen.getByRole("combobox");
			expect(selector).toHaveValue("10");
		});

		it("changes page size", async () => {
			const mockKeys = Array.from({ length: 25 }, (_, i) => ({
				...mockVirtualKey,
				id: `vk-${i}`,
				name: `Key ${i + 1}`,
			}));

			server.use(
				http.get("/api/virtual-keys", () => {
					return HttpResponse.json(mockKeys);
				}),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Key 1")).toBeInTheDocument();
			});

			const selector = screen.getByRole("combobox");
			await user.selectOptions(selector, "20");

			// Should now show 20 items per page
			expect(screen.getByText("Key 20")).toBeInTheDocument();
		});

		it("navigates to next page", async () => {
			const mockKeys = Array.from({ length: 25 }, (_, i) => ({
				...mockVirtualKey,
				id: `vk-${i}`,
				name: `Key ${i + 1}`,
			}));

			server.use(
				http.get("/api/virtual-keys", () => {
					return HttpResponse.json(mockKeys);
				}),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Key 1")).toBeInTheDocument();
			});

			// Verify pagination controls exist
			expect(screen.getByRole("button", { name: "Next" })).toBeInTheDocument();
			expect(screen.getByRole("button", { name: "Prev" })).toBeDisabled();

			// Click Next - should enable Prev button
			await user.click(screen.getByRole("button", { name: "Next" }));

			await waitFor(() => {
				expect(screen.getByRole("button", { name: "Prev" })).not.toBeDisabled();
			});
		});

		it("navigates to previous page", async () => {
			const mockKeys = Array.from({ length: 25 }, (_, i) => ({
				...mockVirtualKey,
				id: `vk-${i}`,
				name: `Key ${i + 1}`,
			}));

			server.use(
				http.get("/api/virtual-keys", () => {
					return HttpResponse.json(mockKeys);
				}),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Key 1")).toBeInTheDocument();
			});

			// Prev should be disabled on first page
			expect(screen.getByRole("button", { name: "Prev" })).toBeDisabled();

			// Click Next to go to page 2
			await user.click(screen.getByRole("button", { name: "Next" }));

			// Now Prev should be enabled
			await waitFor(() => {
				expect(screen.getByRole("button", { name: "Prev" })).not.toBeDisabled();
			});

			// Click Prev to go back to page 1
			await user.click(screen.getByRole("button", { name: "Prev" }));

			// Prev should be disabled again
			await waitFor(() => {
				expect(screen.getByRole("button", { name: "Prev" })).toBeDisabled();
			});
		});

		it("navigates to specific page number", async () => {
			const mockKeys = Array.from({ length: 30 }, (_, i) => ({
				...mockVirtualKey,
				id: `vk-${i}`,
				name: `Key ${i + 1}`,
			}));

			server.use(
				http.get("/api/virtual-keys", () => {
					return HttpResponse.json(mockKeys);
				}),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Key 1")).toBeInTheDocument();
			});

			// Verify pagination controls exist with 30 items (3 pages of 10)
			const nextButton = screen.getByRole("button", { name: "Next" });
			expect(nextButton).toBeInTheDocument();
			expect(screen.getByRole("button", { name: "Prev" })).toBeDisabled();

			// Click Next to go to page 2
			await user.click(nextButton);

			// Prev should now be enabled
			await waitFor(() => {
				expect(screen.getByRole("button", { name: "Prev" })).not.toBeDisabled();
			});

			// Click Next again to go to page 3
			await user.click(screen.getByRole("button", { name: "Next" }));

			// Next should be disabled on last page
			await waitFor(() => {
				expect(screen.getByRole("button", { name: "Next" })).toBeDisabled();
			});
		});

		it("disables Prev button on first page", async () => {
			const mockKeys = Array.from({ length: 15 }, (_, i) => ({
				...mockVirtualKey,
				id: `vk-${i}`,
				name: `Key ${i + 1}`,
			}));

			server.use(
				http.get("/api/virtual-keys", () => {
					return HttpResponse.json(mockKeys);
				}),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Key 1")).toBeInTheDocument();
			});

			expect(screen.getByRole("button", { name: "Prev" })).toBeDisabled();
		});

		it("disables Next button on last page", async () => {
			const mockKeys = Array.from({ length: 15 }, (_, i) => ({
				...mockVirtualKey,
				id: `vk-${i}`,
				name: `Key ${i + 1}`,
			}));

			server.use(
				http.get("/api/virtual-keys", () => {
					return HttpResponse.json(mockKeys);
				}),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Key 1")).toBeInTheDocument();
			});

			// Go to last page
			await user.click(screen.getByRole("button", { name: "2" }));

			await waitFor(() => {
				expect(screen.getByRole("button", { name: "Next" })).toBeDisabled();
			});
		});
	});

	describe("Quick Start Section", () => {
		it("renders quick start guide when keys exist", async () => {
			server.use(...mockVirtualKeys());

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Quick Start")).toBeInTheDocument();
			});

			expect(screen.getByText("Create a Key")).toBeInTheDocument();
			expect(screen.getByText("Copy the Full Key")).toBeInTheDocument();
			expect(screen.getByText("Make Requests")).toBeInTheDocument();
		});

		it("does not render quick start when no keys", async () => {
			server.use(
				http.get("/api/virtual-keys", () => {
					return HttpResponse.json([]);
				}),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(
					screen.getByText(
						"No virtual keys. Create one to start using the proxy.",
					),
				).toBeInTheDocument();
			});

			expect(screen.queryByText("Quick Start")).not.toBeInTheDocument();
		});

		it("renders quick start section with collapsible toggle", async () => {
			server.use(...mockVirtualKeys());

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Quick Start")).toBeInTheDocument();
			});

			// Verify quick start content is visible
			expect(screen.getByText("Create a Key")).toBeInTheDocument();
			expect(screen.getByText("Copy the Full Key")).toBeInTheDocument();
			expect(screen.getByText("Make Requests")).toBeInTheDocument();

			// Toggle button should exist
			const toggleButton = screen.getByRole("button", {
				name: /collapse|expand|toggle/i,
			});
			expect(toggleButton).toBeInTheDocument();
		});

		it("renders bash and PowerShell tab buttons", async () => {
			server.use(...mockVirtualKeys());

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Quick Start")).toBeInTheDocument();
			});

			// Tab bar has buttons with "bash" and "PowerShell" text
			const allButtons = screen.getAllByRole("button");
			const bashButton = allButtons.find((btn) =>
				btn.textContent?.includes("bash"),
			);
			const psButton = allButtons.find((btn) =>
				btn.textContent?.includes("PowerShell"),
			);

			expect(bashButton).toBeInTheDocument();
			expect(psButton).toBeInTheDocument();
		});

		it("shows curl example in bash tab", async () => {
			server.use(...mockVirtualKeys());

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Quick Start")).toBeInTheDocument();
			});

			// curl example is in a code block - check for key parts
			expect(screen.getByText(/curl/)).toBeInTheDocument();
			// URL is in a span element
			expect(
				screen.getByText((content) => content.includes("/v1/chat/completions")),
			).toBeInTheDocument();
			expect(screen.getByText("YOUR_API_KEY")).toBeInTheDocument();
		});

		it("shows PowerShell example in powershell tab", async () => {
			server.use(...mockVirtualKeys());

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Quick Start")).toBeInTheDocument();
			});

			// Click PowerShell tab
			await user.click(screen.getByRole("button", { name: /powershell/i }));

			// Verify PowerShell content is displayed
			expect(screen.getByText(/Invoke-RestMethod/)).toBeInTheDocument();
		});
	});

	describe("API Error Handling", () => {
		it("handles 401 unauthorized error gracefully", async () => {
			// Component uses React Query which handles errors internally.
			// Verifies empty state is rendered on error.
			server.use(
				http.get("/api/virtual-keys", () => {
					return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
				}),
			);

			renderWithProviders(<VirtualKeys />);

			// Component shows empty state when query fails
			await waitFor(
				() => {
					expect(
						screen.getByText(
							"No virtual keys. Create one to start using the proxy.",
						),
					).toBeInTheDocument();
				},
				{ timeout: 5000 },
			);
		});

		it("handles 500 server error gracefully", async () => {
			// Component uses React Query which handles errors internally.
			// Verifies empty state is rendered on error.
			server.use(
				http.get("/api/virtual-keys", () => {
					return HttpResponse.json(
						{ error: "Internal Server Error" },
						{ status: 500 },
					);
				}),
			);

			renderWithProviders(<VirtualKeys />);

			// Component shows empty state when query fails
			await waitFor(
				() => {
					expect(
						screen.getByText(
							"No virtual keys. Create one to start using the proxy.",
						),
					).toBeInTheDocument();
				},
				{ timeout: 5000 },
			);
		});

		it("handles network error gracefully", async () => {
			// Component uses React Query which handles errors internally.
			// Verifies empty state is rendered on error.
			server.use(
				http.get("/api/virtual-keys", () => {
					return HttpResponse.error();
				}),
			);

			renderWithProviders(<VirtualKeys />);

			// Component shows empty state when query fails
			await waitFor(
				() => {
					expect(
						screen.getByText(
							"No virtual keys. Create one to start using the proxy.",
						),
					).toBeInTheDocument();
				},
				{ timeout: 5000 },
			);
		});
	});

	describe("Accessibility", () => {
		it("has proper table structure", async () => {
			server.use(...mockVirtualKeys());

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

			// Table should have proper headers
			expect(
				screen.getByRole("button", { name: /Sort by Name/i }),
			).toBeInTheDocument();
			expect(screen.getByText("Key")).toBeInTheDocument();
			expect(
				screen.getByRole("button", { name: /Sort by Created/i }),
			).toBeInTheDocument();
			expect(
				screen.getByRole("button", { name: /Sort by Tokens/i }),
			).toBeInTheDocument();
			expect(
				screen.getByRole("button", { name: /Sort by Last Used/i }),
			).toBeInTheDocument();
		});

		it("has accessible row buttons", async () => {
			server.use(...mockVirtualKeys());

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

			// Each row should be clickable with role="button"
			const table = screen.getByRole("table");
			const rows = table.querySelectorAll("tbody tr");
			expect(rows).toHaveLength(1);
			expect(rows[0]).toHaveAttribute("role", "button");
		});

		it("has accessible sort buttons with tooltips", async () => {
			server.use(...mockVirtualKeys());

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

			const nameHeader = screen.getByText("Name");
			expect(nameHeader.closest("th")).toHaveAttribute(
				"title",
				"Display name for the virtual key",
			);
		});
	});

	describe("KeyDetailModal unsaved-changes guard", () => {
		it("prompts when closing with unsaved changes and stays open on cancel", async () => {
			server.use(...mockVirtualKeys());

			const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(false);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

			const table = screen.getByRole("table");
			const row = table.querySelector("tbody tr");
			await user.click(row as HTMLElement);

			await waitFor(() => {
				expect(getByDialogName("Virtual Key Details")).toBeInTheDocument();
			});

			// Enter edit mode and make a change
			await user.click(screen.getByRole("button", { name: "Edit" }));
			const nameInput = screen.getByLabelText("Name");
			await user.clear(nameInput);
			await user.type(nameInput, "Modified Name");

			// Click close (X button)
			await user.click(screen.getByRole("button", { name: "Close" }));

			// Confirm should have been called
			expect(confirmSpy).toHaveBeenCalledWith("Discard unsaved changes?");

			// Modal should still be open (user cancelled confirm)
			expect(getByDialogName("Virtual Key Details")).toBeInTheDocument();

			confirmSpy.mockRestore();
		});

		it("closes modal when confirming discard of unsaved changes", async () => {
			server.use(...mockVirtualKeys());

			const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

			const table = screen.getByRole("table");
			const row = table.querySelector("tbody tr");
			await user.click(row as HTMLElement);

			await waitFor(() => {
				expect(getByDialogName("Virtual Key Details")).toBeInTheDocument();
			});

			// Enter edit mode and make a change
			await user.click(screen.getByRole("button", { name: "Edit" }));
			const nameInput = screen.getByLabelText("Name");
			await user.clear(nameInput);
			await user.type(nameInput, "Modified Name");

			// Click close (X button)
			await user.click(screen.getByRole("button", { name: "Close" }));

			// Confirm should have been called
			expect(confirmSpy).toHaveBeenCalledWith("Discard unsaved changes?");

			// Modal should close (user confirmed)
			await waitFor(() => {
				expect(
					screen.queryByRole("dialog", { name: "Virtual Key Details" }),
				).not.toBeInTheDocument();
			});

			confirmSpy.mockRestore();
		});

		it("does not prompt when closing without unsaved changes", async () => {
			server.use(...mockVirtualKeys());

			const confirmSpy = vi.spyOn(window, "confirm");

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

			const table = screen.getByRole("table");
			const row = table.querySelector("tbody tr");
			await user.click(row as HTMLElement);

			await waitFor(() => {
				expect(getByDialogName("Virtual Key Details")).toBeInTheDocument();
			});

			// Close without entering edit mode (no changes)
			await user.click(screen.getByRole("button", { name: "Close" }));

			// Confirm should NOT have been called
			expect(confirmSpy).not.toHaveBeenCalled();

			await waitFor(() => {
				expect(
					screen.queryByRole("dialog", { name: "Virtual Key Details" }),
				).not.toBeInTheDocument();
			});

			confirmSpy.mockRestore();
		});
	});
});
