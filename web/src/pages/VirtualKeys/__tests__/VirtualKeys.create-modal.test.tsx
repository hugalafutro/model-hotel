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

	describe("Create Key Modal", () => {
		it("opens create modal when clicking Create Key button", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Virtual Keys")).toBeInTheDocument();
			});

			const createButton = screen.getByRole("button", {
				name: "Create Key",
			});
			await user.click(createButton);

			await waitFor(() => {
				expect(
					screen.getByRole("dialog", { name: "Create Virtual Key" }),
				).toBeInTheDocument();
			});
		});

		it("creates a new key successfully and shows the key", async () => {
			const newKey = {
				...mockVirtualKey,
				id: "vk-new",
				name: "New Test Key",
				key: "sk_test_newly_created_key_12345",
				key_preview: "sk_test_new••••",
			};

			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
				http.post("/api/virtual-keys", async ({ request }) => {
					const body = await request.json();
					return HttpResponse.json({
						...newKey,
						name: (body as { name: string }).name,
					});
				}),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Virtual Keys")).toBeInTheDocument();
			});

			const createButton = screen.getByRole("button", {
				name: "Create Key",
			});
			await user.click(createButton);

			await waitFor(() => {
				expect(
					screen.getByRole("dialog", { name: "Create Virtual Key" }),
				).toBeInTheDocument();
			});

			const dialog = screen.getByRole("dialog", {
				name: "Create Virtual Key",
			});
			const nameInput = within(dialog).getByLabelText("Name");
			await user.clear(nameInput);
			await user.type(nameInput, "New Test Key");

			const submitButton = within(dialog).getByRole("button", {
				name: "Create Key",
			});
			await user.click(submitButton);

			await waitFor(() => {
				expect(
					screen.getByText(
						"This key cannot be recovered after you close this modal.",
					),
				).toBeInTheDocument();
			});
			expect(
				screen.getByText("sk_test_newly_created_key_12345"),
			).toBeInTheDocument();
		});

		it("creates a new virtual key with rate limits", async () => {
			const newKey = {
				...mockVirtualKey,
				id: "vk-rate-limited",
				name: "Rate Limited Key",
				key: "sk_test_rate_limited_key",
				key_preview: "sk_test_rate••••",
				rate_limit_rps: 50,
				rate_limit_burst: 100,
			};

			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
				http.post("/api/virtual-keys", async ({ request }) => {
					const body = await request.json();
					return HttpResponse.json({
						...newKey,
						name: (body as { name: string }).name,
						rate_limit_rps: (body as { rate_limit_rps: number }).rate_limit_rps,
						rate_limit_burst: (body as { rate_limit_burst: number })
							.rate_limit_burst,
					});
				}),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Virtual Keys")).toBeInTheDocument();
			});

			const createButton = screen.getByRole("button", {
				name: "Create Key",
			});
			await user.click(createButton);

			await waitFor(() => {
				expect(
					screen.getByRole("dialog", { name: "Create Virtual Key" }),
				).toBeInTheDocument();
			});

			const dialog = screen.getByRole("dialog", {
				name: "Create Virtual Key",
			});
			const nameInput = within(dialog).getByLabelText("Name");
			await user.type(nameInput, "Rate Limited Key");

			const rateLimitRpsInput = within(dialog).getByLabelText(
				"Rate Limit RPS (requests/sec)",
			);
			await user.type(rateLimitRpsInput, "50");

			const rateLimitBurstInput = within(dialog).getByLabelText(
				"Rate Limit Burst (max concurrent)",
			);
			await user.type(rateLimitBurstInput, "100");

			const submitButton = within(dialog).getByRole("button", {
				name: "Create Key",
			});
			await user.click(submitButton);

			await waitFor(() => {
				expect(screen.getByText("Virtual Key Created")).toBeInTheDocument();
			});
		});

		it("shows key only once after creation with copy functionality", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
				http.post("/api/virtual-keys", async ({ request }) => {
					const body = await request.json();
					return HttpResponse.json({
						...mockVirtualKey,
						id: "vk-once",
						name: (body as { name: string }).name,
						key: "sk_test_one_time_key",
						key_preview: "sk_test_one••••",
					});
				}),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Virtual Keys")).toBeInTheDocument();
			});

			const createButton = screen.getByRole("button", {
				name: "Create Key",
			});
			await user.click(createButton);

			await waitFor(() => {
				expect(
					screen.getByRole("dialog", { name: "Create Virtual Key" }),
				).toBeInTheDocument();
			});

			const dialog = screen.getByRole("dialog", {
				name: "Create Virtual Key",
			});
			const nameInput = within(dialog).getByLabelText("Name");
			await user.type(nameInput, "One Time Key");

			const submitButton = within(dialog).getByRole("button", {
				name: "Create Key",
			});
			await user.click(submitButton);

			await waitFor(() => {
				expect(screen.getByText("Virtual Key Created")).toBeInTheDocument();
			});

			expect(
				screen.getByText(
					"This key cannot be recovered after you close this modal.",
				),
			).toBeInTheDocument();
			expect(screen.getByText("sk_test_one_time_key")).toBeInTheDocument();
		});

		it("closes modal after clicking Done button", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
				http.post("/api/virtual-keys", async ({ request }) => {
					const body = await request.json();
					return HttpResponse.json({
						...mockVirtualKey,
						id: "vk-done",
						name: (body as { name: string }).name,
					});
				}),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Virtual Keys")).toBeInTheDocument();
			});

			const createButton = screen.getByRole("button", {
				name: "Create Key",
			});
			await user.click(createButton);

			await waitFor(() => {
				expect(
					screen.getByRole("dialog", { name: "Create Virtual Key" }),
				).toBeInTheDocument();
			});

			const dialog = screen.getByRole("dialog", {
				name: "Create Virtual Key",
			});
			const nameInput = within(dialog).getByLabelText("Name");
			await user.type(nameInput, "Test Key");

			const submitButton = within(dialog).getByRole("button", {
				name: "Create Key",
			});
			await user.click(submitButton);

			await waitFor(() => {
				expect(screen.getByText("Virtual Key Created")).toBeInTheDocument();
			});

			const doneButton = within(dialog).getByRole("button", {
				name: "Done",
			});
			await user.click(doneButton);

			await waitFor(() => {
				expect(
					screen.queryByRole("dialog", { name: "Create Virtual Key" }),
				).not.toBeInTheDocument();
			});
		});

		it("shows key copy UI after successful creation", async () => {
			const newKey = {
				...mockVirtualKey,
				id: "vk-new",
				name: "New Test Key",
				key: "sk_test_newly_created_key_12345",
				key_preview: "sk_test_new••••",
			};

			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
				http.post("/api/virtual-keys", async ({ request }) => {
					const body = await request.json();
					return HttpResponse.json({
						...newKey,
						name: (body as { name: string }).name,
					});
				}),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Virtual Keys")).toBeInTheDocument();
			});

			const createButton = screen.getByRole("button", {
				name: "Create Key",
			});
			await user.click(createButton);

			await waitFor(() => {
				expect(
					screen.getByRole("dialog", { name: "Create Virtual Key" }),
				).toBeInTheDocument();
			});

			const dialog = screen.getByRole("dialog", {
				name: "Create Virtual Key",
			});
			const nameInput = within(dialog).getByLabelText("Name");
			await user.clear(nameInput);
			await user.type(nameInput, "New Test Key");

			const submitButton = within(dialog).getByRole("button", {
				name: "Create Key",
			});
			await user.click(submitButton);

			await waitFor(() => {
				expect(
					screen.getByText(
						"This key cannot be recovered after you close this modal.",
					),
				).toBeInTheDocument();
			});
			expect(
				screen.getByText("sk_test_newly_created_key_12345"),
			).toBeInTheDocument();
		});

		it("shows error toast when create fails", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
				http.post("/api/virtual-keys", () =>
					HttpResponse.json({ error: "Name is required" }, { status: 400 }),
				),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Virtual Keys")).toBeInTheDocument();
			});

			const createButton = screen.getByRole("button", {
				name: "Create Key",
			});
			await user.click(createButton);

			await waitFor(() => {
				expect(
					screen.getByRole("dialog", { name: "Create Virtual Key" }),
				).toBeInTheDocument();
			});

			const dialog = screen.getByRole("dialog", {
				name: "Create Virtual Key",
			});
			const nameInput = within(dialog).getByLabelText("Name");
			await user.clear(nameInput);
			await user.type(nameInput, "Test Key");

			const submitButton = within(dialog).getByRole("button", {
				name: "Create Key",
			});
			await user.click(submitButton);

			await waitFor(() => {
				expect(
					screen.getByText(/Failed:.*Name is required/i),
				).toBeInTheDocument();
			});
		});

		it("closes create modal when clicking Cancel button", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Virtual Keys")).toBeInTheDocument();
			});

			const createButton = screen.getByRole("button", {
				name: "Create Key",
			});
			await user.click(createButton);

			await waitFor(() => {
				expect(
					screen.getByRole("dialog", { name: "Create Virtual Key" }),
				).toBeInTheDocument();
			});

			const dialog = screen.getByRole("dialog", {
				name: "Create Virtual Key",
			});
			const cancelButton = within(dialog).getByRole("button", {
				name: "Cancel",
			});
			await user.click(cancelButton);

			await waitFor(() => {
				expect(
					screen.queryByRole("dialog", { name: "Create Virtual Key" }),
				).not.toBeInTheDocument();
			});
		});

		it("validates name field is required", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Virtual Keys")).toBeInTheDocument();
			});

			const createButton = screen.getByRole("button", {
				name: "Create Key",
			});
			await user.click(createButton);

			await waitFor(() => {
				expect(
					screen.getByRole("dialog", { name: "Create Virtual Key" }),
				).toBeInTheDocument();
			});

			// Try to submit without name
			const dialog = screen.getByRole("dialog", {
				name: "Create Virtual Key",
			});
			const submitButton = within(dialog).getByRole("button", {
				name: "Create Key",
			});
			await user.click(submitButton);

			// Form should not submit (name is required with HTML5 validation)
			await waitFor(() => {
				expect(
					screen.queryByText("Virtual Key Created"),
				).not.toBeInTheDocument();
			});
		});

		it("shows provider access section in create modal", async () => {
			server.use(
				http.get("/api/providers", () => HttpResponse.json([mockProvider])),
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Virtual Keys")).toBeInTheDocument();
			});

			const createButton = screen.getByRole("button", {
				name: "Create Key",
			});
			await user.click(createButton);

			await waitFor(() => {
				expect(
					screen.getByRole("dialog", { name: "Create Virtual Key" }),
				).toBeInTheDocument();
			});

			const dialog = screen.getByRole("dialog", {
				name: "Create Virtual Key",
			});

			// Check Provider Access label exists
			expect(
				within(dialog).getByText("Provider Access", { exact: false }),
			).toBeInTheDocument();

			// Provider name appears as a tag chip
			expect(within(dialog).getByText("Test Provider")).toBeInTheDocument();
		});

		it("toggles provider selection in create modal", async () => {
			server.use(
				http.get("/api/providers", () => HttpResponse.json([mockProvider])),
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Virtual Keys")).toBeInTheDocument();
			});

			const createButton = screen.getByRole("button", {
				name: "Create Key",
			});
			await user.click(createButton);

			await waitFor(() => {
				expect(
					screen.getByRole("dialog", { name: "Create Virtual Key" }),
				).toBeInTheDocument();
			});

			const dialog = screen.getByRole("dialog", {
				name: "Create Virtual Key",
			});

			// Find provider button - should have aria-pressed="false" initially (not excluded)
			const providerButton = within(dialog).getByRole("button", {
				name: "Test Provider",
			});
			expect(providerButton).toHaveAttribute("aria-pressed", "false");

			// Click provider tag to exclude it
			await user.click(providerButton);

			// Should now be excluded (aria-pressed="true")
			expect(providerButton).toHaveAttribute("aria-pressed", "true");
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
				expect(screen.getByText("Virtual Keys")).toBeInTheDocument();
			});

			const createButton = screen.getByRole("button", {
				name: "Create Key",
			});
			await user.click(createButton);

			await waitFor(() => {
				expect(
					screen.getByRole("dialog", { name: "Create Virtual Key" }),
				).toBeInTheDocument();
			});

			const dialog = screen.getByRole("dialog", {
				name: "Create Virtual Key",
			});

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

		it("sends allowed_providers on create", async () => {
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

			let createBody: unknown;
			server.use(
				http.get("/api/providers", () => HttpResponse.json(mockProviders)),
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
				http.post("/api/virtual-keys", async ({ request }) => {
					createBody = await request.json();
					return HttpResponse.json({
						...mockVirtualKeyWithProviders,
						id: "vk-new-with-providers",
						name: (createBody as { name: string }).name,
						allowed_providers: (createBody as { allowed_providers: string[] })
							.allowed_providers,
					});
				}),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Virtual Keys")).toBeInTheDocument();
			});

			const createButton = screen.getByRole("button", {
				name: "Create Key",
			});
			await user.click(createButton);

			await waitFor(() => {
				expect(
					screen.getByRole("dialog", { name: "Create Virtual Key" }),
				).toBeInTheDocument();
			});

			const dialog = screen.getByRole("dialog", {
				name: "Create Virtual Key",
			});

			// Enter name
			const nameInput = within(dialog).getByLabelText("Name");
			await user.type(nameInput, "Key with Provider Access");

			// Exclude provider-002 (Other Provider)
			const otherProviderButton = within(dialog).getByRole("button", {
				name: "Other Provider",
			});
			await user.click(otherProviderButton);
			expect(otherProviderButton).toHaveAttribute("aria-pressed", "true");

			// Click Create Key
			const submitButton = within(dialog).getByRole("button", {
				name: "Create Key",
			});
			await user.click(submitButton);

			await waitFor(() => {
				expect(screen.getByText("Virtual Key Created")).toBeInTheDocument();
			});

			// Verify the create request included allowed_providers
			// After excluding provider-002, only provider-001 should be allowed
			expect(createBody).toBeDefined();
			const body = createBody as { allowed_providers?: string[] };
			expect(body.allowed_providers).toEqual(["provider-001"]);
		});

		it("toggles strip reasoning off by default and can be enabled", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Virtual Keys")).toBeInTheDocument();
			});

			const createButton = screen.getByRole("button", {
				name: "Create Key",
			});
			await user.click(createButton);

			await waitFor(() => {
				expect(
					screen.getByRole("dialog", { name: "Create Virtual Key" }),
				).toBeInTheDocument();
			});

			const dialog = screen.getByRole("dialog", {
				name: "Create Virtual Key",
			});

			const toggle = within(dialog).getByRole("switch", {
				name: "Strip Reasoning",
			});
			expect(toggle).toHaveAttribute("aria-checked", "false");
			expect(within(dialog).getByText("Disabled")).toBeInTheDocument();

			await user.click(toggle);

			expect(toggle).toHaveAttribute("aria-checked", "true");
			expect(within(dialog).getByText("Enabled")).toBeInTheDocument();
		});

		it("sends strip_reasoning in create request when enabled", async () => {
			let createBody: unknown;
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
				http.post("/api/virtual-keys", async ({ request }) => {
					createBody = await request.json();
					return HttpResponse.json({
						...mockVirtualKey,
						id: "vk-strip",
						name: (createBody as { name: string }).name,
					});
				}),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Virtual Keys")).toBeInTheDocument();
			});

			const createButton = screen.getByRole("button", {
				name: "Create Key",
			});
			await user.click(createButton);

			await waitFor(() => {
				expect(
					screen.getByRole("dialog", { name: "Create Virtual Key" }),
				).toBeInTheDocument();
			});

			const dialog = screen.getByRole("dialog", {
				name: "Create Virtual Key",
			});

			const nameInput = within(dialog).getByLabelText("Name");
			await user.type(nameInput, "Strip Key");

			const toggle = within(dialog).getByRole("switch", {
				name: "Strip Reasoning",
			});
			await user.click(toggle);

			const submitButton = within(dialog).getByRole("button", {
				name: "Create Key",
			});
			await user.click(submitButton);

			await waitFor(() => {
				expect(screen.getByText("Virtual Key Created")).toBeInTheDocument();
			});

			expect(createBody).toBeDefined();
			expect(
				(createBody as { strip_reasoning?: boolean }).strip_reasoning,
			).toBe(true);
		});

		it("renders providers in alphabetical order", async () => {
			const betaProvider = {
				...mockProvider,
				id: "provider-beta",
				name: "Beta Provider",
				created_at: "2026-02-20T10:00:00Z",
				updated_at: "2026-05-11T12:00:00Z",
			};
			const alphaProvider = {
				...mockProvider,
				id: "provider-alpha",
				name: "Alpha Provider",
				created_at: "2026-02-20T10:00:00Z",
				updated_at: "2026-05-11T12:00:00Z",
			};

			server.use(
				http.get("/api/providers", () =>
					HttpResponse.json([betaProvider, alphaProvider]),
				),
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Virtual Keys")).toBeInTheDocument();
			});

			const createButton = screen.getByRole("button", {
				name: "Create Key",
			});
			await user.click(createButton);

			await waitFor(() => {
				expect(
					screen.getByRole("dialog", { name: "Create Virtual Key" }),
				).toBeInTheDocument();
			});

			const dialog = screen.getByRole("dialog", {
				name: "Create Virtual Key",
			});

			const providerButtons = within(dialog)
				.getAllByRole("button")
				.filter(
					(btn) =>
						btn.getAttribute("aria-pressed") !== null &&
						btn.textContent?.trim(),
				);
			const names = providerButtons.map((btn) => btn.textContent);
			expect(names).toEqual(["Alpha Provider", "Beta Provider"]);
		});

		it("shows error when all providers are excluded on create", async () => {
			const mockProviders = [mockProvider, mockProvider2];

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

			const createButton = screen.getByRole("button", {
				name: "Create Key",
			});
			await user.click(createButton);

			const dialog = screen.getByRole("dialog", {
				name: "Create Virtual Key",
			});

			// Fill in name
			const nameInput = within(dialog).getByLabelText("Name");
			await user.type(nameInput, "My Key");

			// Exclude all providers
			for (const provider of mockProviders) {
				const chip = within(dialog).getByRole("button", {
					name: provider.name,
				});
				await user.click(chip);
			}

			// Click Create
			const submitButton = within(dialog).getByRole("button", {
				name: "Create Key",
			});
			await user.click(submitButton);

			// Should show error message, not create the key
			await waitFor(() => {
				expect(
					screen.getByText("At least one provider must remain accessible"),
				).toBeInTheDocument();
			});
		});

		it("reveals usage examples prefilled with the real key after creation", async () => {
			const createdKeyValue = "sk_test_newly_created_key_12345";
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
				http.post("/api/virtual-keys", async ({ request }) => {
					const body = await request.json();
					return HttpResponse.json({
						...mockVirtualKey,
						id: "vk-examples",
						name: (body as { name: string }).name,
						key: createdKeyValue,
						key_preview: "sk_test_new••••",
					});
				}),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Virtual Keys")).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: "Create Key" }));

			const dialog = await screen.findByRole("dialog", {
				name: "Create Virtual Key",
			});
			await user.type(within(dialog).getByLabelText("Name"), "Examples Key");
			await user.click(
				within(dialog).getByRole("button", { name: "Create Key" }),
			);

			await waitFor(() => {
				expect(screen.getByText("Virtual Key Created")).toBeInTheDocument();
			});

			// Examples are collapsed by default: no snippet copy button yet.
			expect(
				within(dialog).queryByRole("button", { name: /Copy cURL snippet/i }),
			).not.toBeInTheDocument();

			// Expand the disclosure.
			await user.click(
				within(dialog).getByRole("button", { name: "Usage examples" }),
			);

			// Snippet windows now render, prefilled with the real key and with no
			// leftover YOUR_API_KEY placeholder.
			expect(
				within(dialog).getByRole("button", { name: /Copy cURL snippet/i }),
			).toBeInTheDocument();
			expect(
				within(dialog).getAllByText((content) =>
					content.includes(createdKeyValue),
				).length,
			).toBeGreaterThan(1);
			expect(
				within(dialog).queryByText((content) =>
					content.includes("YOUR_API_KEY"),
				),
			).not.toBeInTheDocument();
		});
	});
});
