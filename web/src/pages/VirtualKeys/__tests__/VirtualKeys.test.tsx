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

	describe("Loading State", () => {
		it("renders loading spinner initially", () => {
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
	});

	describe("Empty State", () => {
		it("renders empty state when no virtual keys exist", async () => {
			server.use(http.get("/api/virtual-keys", () => HttpResponse.json([])));

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(
					screen.getByText(
						"No virtual keys. Create one to start using the proxy.",
					),
				).toBeInTheDocument();
			});
		});
	});

	describe("Page Header", () => {
		it("renders page header with correct title and create button", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("1 Virtual Key")).toBeInTheDocument();
			});
			expect(
				screen.getByRole("button", { name: "+ Create Key" }),
			).toBeInTheDocument();
		});

		it("displays plural title for multiple keys", async () => {
			const keys = [
				mockVirtualKey,
				{ ...mockVirtualKey, id: "vk-002", name: "Second Key" },
			];
			server.use(http.get("/api/virtual-keys", () => HttpResponse.json(keys)));

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("2 Virtual Keys")).toBeInTheDocument();
			});
		});

		it("renders proxy URL as copyable pill in header subtitle", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

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
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "+ Create Key" }),
				).toBeInTheDocument();
			});
		});
	});

	describe("Virtual Keys Table", () => {
		it("renders table with virtual key data", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});
			expect(screen.getByText("sk_test_••••")).toBeInTheDocument();
			expect(screen.getByText("50,000")).toBeInTheDocument();
		});

		it("renders RPS and Burst columns with values", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});
			// RPS column shows value when set
			expect(screen.getByText("30")).toBeInTheDocument();
			// Burst column shows value when set
			expect(screen.getByText("60")).toBeInTheDocument();
		});

		it("shows Global for RPS and Burst when null", async () => {
			const keyWithNullLimits = {
				...mockVirtualKey,
				id: "vk-null-limits",
				name: "No Limits Key",
				rate_limit_rps: null,
				rate_limit_burst: null,
			};
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([keyWithNullLimits]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("No Limits Key")).toBeInTheDocument();
			});
			// RPS column shows "Global" when null
			expect(screen.getAllByText("Global")).toHaveLength(2);
		});

		it("clicking row opens detail modal", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

			// Click on the name cell to open detail modal (row click)
			const nameCell = screen.getByText("Test API Key");
			await user.click(nameCell);

			await waitFor(() => {
				expect(
					screen.getByRole("dialog", { name: "Virtual Key Details" }),
				).toBeInTheDocument();
			});

			// Verify modal content (query within dialog to avoid ambiguity)
			const dialog = screen.getByRole("dialog", {
				name: "Virtual Key Details",
			});
			expect(within(dialog).getByText("Test API Key")).toBeInTheDocument();
			expect(within(dialog).getByText("sk_test_••••")).toBeInTheDocument();
			expect(within(dialog).getByText("30")).toBeInTheDocument();
			expect(within(dialog).getByText("60")).toBeInTheDocument();
		});

		it("opens edit mode from detail modal and updates key", async () => {
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

			// Verify edit mode inputs
			const nameInput = within(dialog).getByLabelText("Name");
			expect(nameInput).toHaveValue("Test API Key");

			const rateLimitRpsInput = within(dialog).getByLabelText(
				"Rate Limit RPS (requests/sec)",
			);
			expect(rateLimitRpsInput).toHaveValue(30);

			const rateLimitBurstInput = within(dialog).getByLabelText(
				"Rate Limit Burst (max concurrent)",
			);
			expect(rateLimitBurstInput).toHaveValue(60);
		});
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
				expect(screen.getByText("1 Virtual Key")).toBeInTheDocument();
			});

			const createButton = screen.getByRole("button", {
				name: "+ Create Key",
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
				expect(screen.getByText("1 Virtual Key")).toBeInTheDocument();
			});

			const createButton = screen.getByRole("button", {
				name: "+ Create Key",
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
					screen.getByText("Copy this key now. It won't be shown again."),
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
				expect(screen.getByText("1 Virtual Key")).toBeInTheDocument();
			});

			const createButton = screen.getByRole("button", {
				name: "+ Create Key",
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
				expect(screen.getByText("1 Virtual Key")).toBeInTheDocument();
			});

			const createButton = screen.getByRole("button", {
				name: "+ Create Key",
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
				screen.getByText("Copy this key now. It won't be shown again."),
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
				expect(screen.getByText("1 Virtual Key")).toBeInTheDocument();
			});

			const createButton = screen.getByRole("button", {
				name: "+ Create Key",
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
				expect(screen.getByText("1 Virtual Key")).toBeInTheDocument();
			});

			const createButton = screen.getByRole("button", {
				name: "+ Create Key",
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
					screen.getByText("Copy this key now. It won't be shown again."),
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
				expect(screen.getByText("1 Virtual Key")).toBeInTheDocument();
			});

			const createButton = screen.getByRole("button", {
				name: "+ Create Key",
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
				expect(screen.getByText("1 Virtual Key")).toBeInTheDocument();
			});

			const createButton = screen.getByRole("button", {
				name: "+ Create Key",
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
				expect(screen.getByText("1 Virtual Key")).toBeInTheDocument();
			});

			const createButton = screen.getByRole("button", {
				name: "+ Create Key",
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

			// Update Burst
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

	describe("Sorting", () => {
		it("sorts by name ascending by default", async () => {
			const keys = [
				{ ...mockVirtualKey, id: "vk-001", name: "Zebra Key" },
				{ ...mockVirtualKey, id: "vk-002", name: "Alpha Key" },
				{ ...mockVirtualKey, id: "vk-003", name: "Beta Key" },
			];
			server.use(http.get("/api/virtual-keys", () => HttpResponse.json(keys)));

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Alpha Key")).toBeInTheDocument();
			});

			// Check order: query rows within table (they have role="button")
			const table = screen.getByRole("table");
			const rows = within(table).getAllByRole("button", { name: /.* Key/ });
			expect(rows).toHaveLength(3);
			expect(rows[0]).toHaveTextContent("Alpha Key");
			expect(rows[1]).toHaveTextContent("Beta Key");
			expect(rows[2]).toHaveTextContent("Zebra Key");
		});

		it("toggles sort direction when clicking header", async () => {
			const keys = [
				{ ...mockVirtualKey, id: "vk-001", name: "Alpha Key" },
				{ ...mockVirtualKey, id: "vk-002", name: "Zebra Key" },
			];
			server.use(http.get("/api/virtual-keys", () => HttpResponse.json(keys)));

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Alpha Key")).toBeInTheDocument();
			});

			const nameHeader = screen.getByRole("button", {
				name: "Sort by Name",
			});
			await user.click(nameHeader);

			await waitFor(() => {
				// After clicking, should be descending: Zebra first
				const table = screen.getByRole("table");
				const rows = within(table).getAllByRole("button", { name: /.* Key/ });
				expect(rows[0]).toHaveTextContent("Zebra Key");
			});
		});

		it("sorts by created date", async () => {
			const keys = [
				{
					...mockVirtualKey,
					id: "vk-001",
					name: "Old Key",
					created_at: "2026-01-01T00:00:00Z",
				},
				{
					...mockVirtualKey,
					id: "vk-002",
					name: "New Key",
					created_at: "2026-06-01T00:00:00Z",
				},
			];
			server.use(http.get("/api/virtual-keys", () => HttpResponse.json(keys)));

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Old Key")).toBeInTheDocument();
			});

			const createdHeader = screen.getByRole("button", {
				name: "Sort by Created",
			});
			await user.click(createdHeader); // first click: asc
			await user.click(createdHeader); // second click: desc

			await waitFor(() => {
				// After two clicks, should be descending: New first
				const allNames = screen.getAllByText(/Key$/);
				const nameCells = allNames.filter(
					(el) => el.tagName === "TD" || el.parentElement?.tagName === "TD",
				);
				expect(nameCells[0]).toHaveTextContent("New Key");
			});
		});

		it("sorts by tokens", async () => {
			const keys = [
				{
					...mockVirtualKey,
					id: "vk-001",
					name: "Low Tokens",
					tokens_used: 1000,
				},
				{
					...mockVirtualKey,
					id: "vk-002",
					name: "High Tokens",
					tokens_used: 100000,
				},
			];
			server.use(http.get("/api/virtual-keys", () => HttpResponse.json(keys)));

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Low Tokens")).toBeInTheDocument();
			});

			const tokensHeader = screen.getByRole("button", {
				name: "Sort by Tokens",
			});
			await user.click(tokensHeader); // first click: asc
			await user.click(tokensHeader); // second click: desc

			await waitFor(() => {
				// After two clicks, should be descending: High first
				const allNames = screen.getAllByText(/Tokens$/);
				const nameCells = allNames.filter(
					(el) => el.tagName === "TD" || el.parentElement?.tagName === "TD",
				);
				expect(nameCells[0]).toHaveTextContent("High Tokens");
			});
		});

		it("sorts by last_used", async () => {
			const keys = [
				{
					...mockVirtualKey,
					id: "vk-001",
					name: "Old Used",
					last_used_at: "2026-01-01T00:00:00Z",
				},
				{
					...mockVirtualKey,
					id: "vk-002",
					name: "Recent Used",
					last_used_at: "2026-06-01T00:00:00Z",
				},
			];
			server.use(http.get("/api/virtual-keys", () => HttpResponse.json(keys)));

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Old Used")).toBeInTheDocument();
			});

			const lastUsedHeader = screen.getByRole("button", {
				name: "Sort by Last Used",
			});
			await user.click(lastUsedHeader); // first click: asc
			await user.click(lastUsedHeader); // second click: desc

			await waitFor(() => {
				// After two clicks, should be descending: Recent first
				const allNames = screen.getAllByText(/Used$/);
				const nameCells = allNames.filter(
					(el) => el.tagName === "TD" || el.parentElement?.tagName === "TD",
				);
				expect(nameCells[0]).toHaveTextContent("Recent Used");
			});
		});

		it("sorts by name ascending", async () => {
			const keys = [
				{ ...mockVirtualKey, id: "vk-001", name: "Zebra Key" },
				{ ...mockVirtualKey, id: "vk-002", name: "Alpha Key" },
				{ ...mockVirtualKey, id: "vk-003", name: "Beta Key" },
			];
			server.use(http.get("/api/virtual-keys", () => HttpResponse.json(keys)));

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Zebra Key")).toBeInTheDocument();
			});

			// Initial sort is name ascending, so data is already sorted
			// Click twice to get back to ascending (first click -> desc, second -> asc)
			await user.click(screen.getByRole("button", { name: "Sort by Name" }));
			await user.click(screen.getByRole("button", { name: "Sort by Name" }));

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
			const keys = [
				{ ...mockVirtualKey, id: "vk-001", name: "Alpha Key" },
				{ ...mockVirtualKey, id: "vk-002", name: "Beta Key" },
			];
			server.use(http.get("/api/virtual-keys", () => HttpResponse.json(keys)));

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Alpha Key")).toBeInTheDocument();
			});

			// Default sort is name ascending, click once for descending
			await user.click(screen.getByRole("button", { name: "Sort by Name" }));

			// Should be sorted descending: Beta, Alpha
			const table = screen.getByRole("table");
			const rows = table.querySelectorAll("tbody tr");
			expect(rows).toHaveLength(2);
			// First should be Beta, second Alpha (descending)
			expect(rows[0].querySelector("td")?.textContent).toBe("Beta Key");
			expect(rows[1].querySelector("td")?.textContent).toBe("Alpha Key");
		});

		it("sorts by created date", async () => {
			const keys = [
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
			server.use(http.get("/api/virtual-keys", () => HttpResponse.json(keys)));

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("New Key")).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: "Sort by Created" }));

			// Old key should be first (ascending by date)
			const table = screen.getByRole("table");
			const rows = table.querySelectorAll("tbody tr");
			expect(rows[0].querySelector("td")?.textContent).toBe("Old Key");
		});

		it("sorts by tokens used", async () => {
			const keys = [
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
			server.use(http.get("/api/virtual-keys", () => HttpResponse.json(keys)));

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("High Usage")).toBeInTheDocument();
			});

			await user.click(screen.getByRole("button", { name: "Sort by Tokens" }));

			const table = screen.getByRole("table");
			const rows = table.querySelectorAll("tbody tr");
			expect(rows[0].querySelector("td")?.textContent).toBe("Low Usage");
		});

		it("sorts by last used", async () => {
			const keys = [
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
			server.use(http.get("/api/virtual-keys", () => HttpResponse.json(keys)));

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Recent")).toBeInTheDocument();
			});

			await user.click(
				screen.getByRole("button", { name: "Sort by Last Used" }),
			);

			const table = screen.getByRole("table");
			const rows = table.querySelectorAll("tbody tr");
			expect(rows[0].querySelector("td")?.textContent).toBe("Old");
		});

		it("Key column header is not a sortable button", async () => {
			const keys = [
				{ ...mockVirtualKey, id: "vk-001", name: "Key A" },
				{ ...mockVirtualKey, id: "vk-002", name: "Key B" },
			];
			server.use(http.get("/api/virtual-keys", () => HttpResponse.json(keys)));

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Key A")).toBeInTheDocument();
			});

			// Verify both keys are in the table
			expect(screen.getByText("Key A")).toBeInTheDocument();
			expect(screen.getByText("Key B")).toBeInTheDocument();

			// Key header is a columnheader, not a button
			const keyHeader = screen.getByRole("columnheader", {
				name: "Key",
			});
			expect(keyHeader).toBeInTheDocument();
			// Verify it's not a button (no sort functionality)
			expect(keyHeader.tagName).toBe("TH");
			expect(() =>
				screen.getByRole("button", { name: "Sort by Key" }),
			).toThrow();
		});
	});

	describe("Pagination", () => {
		it("renders pagination bar when there are keys", async () => {
			const keys = Array.from({ length: 15 }, (_, i) => ({
				...mockVirtualKey,
				id: `vk-${String(i + 1).padStart(3, "0")}`,
				name: `Key ${i + 1}`,
			}));
			server.use(http.get("/api/virtual-keys", () => HttpResponse.json(keys)));

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("15 Virtual Keys")).toBeInTheDocument();
			});

			// Pagination shows "X to Y of Z keys" format
			expect(screen.getByText("1 to 10 of 15 keys")).toBeInTheDocument();
			// Page buttons show just the number, not "Page N"
			expect(screen.getByRole("button", { name: "1" })).toBeInTheDocument();
		});

		it("renders pagination bar with multiple keys", async () => {
			const keys = Array.from({ length: 15 }, (_, i) => ({
				...mockVirtualKey,
				id: `vk-${String(i + 1).padStart(3, "0")}`,
				name: `Key ${i + 1}`,
			}));
			server.use(http.get("/api/virtual-keys", () => HttpResponse.json(keys)));

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Key 1")).toBeInTheDocument();
			});

			expect(screen.getByRole("button", { name: "Prev" })).toBeInTheDocument();
			expect(screen.getByRole("button", { name: "Next" })).toBeInTheDocument();
		});

		it("shows correct page size selector", async () => {
			const keys = Array.from({ length: 25 }, (_, i) => ({
				...mockVirtualKey,
				id: `vk-${String(i + 1).padStart(3, "0")}`,
				name: `Key ${i + 1}`,
			}));
			server.use(http.get("/api/virtual-keys", () => HttpResponse.json(keys)));

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Key 1")).toBeInTheDocument();
			});

			const selector = screen.getByRole("combobox");
			expect(selector).toHaveValue("10");
		});

		it("changes page size", async () => {
			const keys = Array.from({ length: 25 }, (_, i) => ({
				...mockVirtualKey,
				id: `vk-${String(i + 1).padStart(3, "0")}`,
				name: `Key ${i + 1}`,
			}));
			server.use(http.get("/api/virtual-keys", () => HttpResponse.json(keys)));

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("25 Virtual Keys")).toBeInTheDocument();
			});

			// The select has no aria-label; find by role
			const pageSizeSelect = screen.getByRole("combobox");
			// PaginationBar options: 10, 20, 30, 40, 50
			await user.selectOptions(pageSizeSelect, "20");

			await waitFor(() => {
				expect(screen.getByText("1 to 20 of 25 keys")).toBeInTheDocument();
			});
		});

		it("navigates to next page", async () => {
			const keys = Array.from({ length: 15 }, (_, i) => ({
				...mockVirtualKey,
				id: `vk-${String(i + 1).padStart(3, "0")}`,
				name: `Key ${i + 1}`,
			}));
			server.use(http.get("/api/virtual-keys", () => HttpResponse.json(keys)));

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("15 Virtual Keys")).toBeInTheDocument();
			});

			const nextPageButton = screen.getByRole("button", {
				name: "Next",
			});
			await user.click(nextPageButton);

			await waitFor(() => {
				expect(screen.getByText("11 to 15 of 15 keys")).toBeInTheDocument();
				expect(screen.getByRole("button", { name: "2" })).toHaveClass(
					"bg-(--accent)",
				);
			});
		});

		it("navigates to previous page", async () => {
			const keys = Array.from({ length: 25 }, (_, i) => ({
				...mockVirtualKey,
				id: `vk-${String(i + 1).padStart(3, "0")}`,
				name: `Key ${i + 1}`,
			}));
			server.use(http.get("/api/virtual-keys", () => HttpResponse.json(keys)));

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("25 Virtual Keys")).toBeInTheDocument();
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
			const keys = Array.from({ length: 30 }, (_, i) => ({
				...mockVirtualKey,
				id: `vk-${String(i + 1).padStart(3, "0")}`,
				name: `Key ${i + 1}`,
			}));
			server.use(http.get("/api/virtual-keys", () => HttpResponse.json(keys)));

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
			const keys = Array.from({ length: 15 }, (_, i) => ({
				...mockVirtualKey,
				id: `vk-${String(i + 1).padStart(3, "0")}`,
				name: `Key ${i + 1}`,
			}));
			server.use(http.get("/api/virtual-keys", () => HttpResponse.json(keys)));

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Key 1")).toBeInTheDocument();
			});

			expect(screen.getByRole("button", { name: "Prev" })).toBeDisabled();
		});

		it("disables Next button on last page", async () => {
			const keys = Array.from({ length: 15 }, (_, i) => ({
				...mockVirtualKey,
				id: `vk-${String(i + 1).padStart(3, "0")}`,
				name: `Key ${i + 1}`,
			}));
			server.use(http.get("/api/virtual-keys", () => HttpResponse.json(keys)));

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
		it("renders quick start guide with bash terminal by default", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Quick Start")).toBeInTheDocument();
			});

			// Tab buttons contain SVG + text; use getAllByRole and filter by class
			const bashTabs = screen.getAllByRole("button", { name: /bash/i });
			// First match is the tab (has terminal-tab class), not CopyButton
			const bashTab = bashTabs.find((btn) =>
				btn.classList.contains("terminal-tab"),
			);
			expect(bashTab).toHaveClass("terminal-tab-active");
			const powershellTabs = screen.getAllByRole("button", {
				name: /PowerShell/i,
			});
			const powershellTab = powershellTabs.find((btn) =>
				btn.classList.contains("terminal-tab"),
			);
			expect(powershellTab).toHaveClass("terminal-tab-inactive");
		});

		it("renders quick start guide when keys exist", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Quick Start")).toBeInTheDocument();
			});

			expect(screen.getByText("Create a Key")).toBeInTheDocument();
			expect(screen.getByText("Copy the Full Key")).toBeInTheDocument();
			expect(screen.getByText("Make Requests")).toBeInTheDocument();
		});

		it("does not render quick start when no keys", async () => {
			server.use(http.get("/api/virtual-keys", () => HttpResponse.json([])));

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
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

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
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

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
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

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
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Quick Start")).toBeInTheDocument();
			});

			// Click PowerShell tab
			await user.click(screen.getByRole("button", { name: /powershell/i }));

			// Verify PowerShell content is displayed
			expect(screen.getByText(/Invoke-RestMethod/)).toBeInTheDocument();
		});

		it("renders CopyButton in terminal tab bar", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Quick Start")).toBeInTheDocument();
			});

			// CopyButton has title attribute with snippet type
			expect(
				screen.getByRole("button", { name: /Copy bash snippet/i }),
			).toBeInTheDocument();
		});

		it("CopyButton updates when switching to PowerShell tab", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Quick Start")).toBeInTheDocument();
			});

			const powershellTab = screen.getByRole("button", {
				name: /PowerShell/i,
			});
			await user.click(powershellTab);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: /Copy PowerShell snippet/i }),
				).toBeInTheDocument();
			});
		});

		it("switches to PowerShell tab when clicked (line 587)", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Quick Start")).toBeInTheDocument();
			});

			const powershellTabs = screen.getAllByRole("button", {
				name: /PowerShell/i,
			});
			const powershellTab = powershellTabs.find((btn) =>
				btn.classList.contains("terminal-tab"),
			);
			if (!powershellTab) throw new Error("PowerShell tab not found");
			await user.click(powershellTab);

			await waitFor(() => {
				expect(powershellTab).toHaveClass("terminal-tab-active");
			});
			const bashTabs = screen.getAllByRole("button", { name: /bash/i });
			const bashTab = bashTabs.find((btn) =>
				btn.classList.contains("terminal-tab"),
			);
			expect(bashTab).toHaveClass("terminal-tab-inactive");
		});

		it("collapses quick start section", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Quick Start")).toBeInTheDocument();
			});

			// CollapsibleToggle defaults: aria-label="Collapse" when expanded
			const collapseToggle = screen.getByRole("button", {
				name: "Collapse",
			});
			await user.click(collapseToggle);

			// After clicking, button label changes to "Expand"
			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "Expand" }),
				).toBeInTheDocument();
			});
		});

		it("renders JavaScript example card", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Quick Start")).toBeInTheDocument();
			});

			// JavaScript title appears in card header; verify CopyButton exists
			expect(
				screen.getByRole("button", { name: "Copy JavaScript snippet" }),
			).toBeInTheDocument();
			// Check title exists (may appear multiple times due to code content)
			const jsTitles = screen.getAllByText("JavaScript");
			expect(jsTitles.length).toBeGreaterThan(0);
		});

		it("renders Python example card", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Quick Start")).toBeInTheDocument();
			});

			// Python title appears in card header; verify CopyButton exists
			expect(
				screen.getByRole("button", { name: "Copy Python snippet" }),
			).toBeInTheDocument();
			// Check title exists (may appear multiple times due to code content)
			const pythonTitles = screen.getAllByText("Python");
			expect(pythonTitles.length).toBeGreaterThan(0);
		});

		it("renders Claude Code example card", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Quick Start")).toBeInTheDocument();
			});

			// Claude Code title appears in card header; verify CopyButton exists
			expect(
				screen.getByRole("button", { name: "Copy Claude Code snippet" }),
			).toBeInTheDocument();
			// Check title exists (may appear multiple times due to code content)
			const claudeCodeTitles = screen.getAllByText("Claude Code");
			expect(claudeCodeTitles.length).toBeGreaterThan(0);
		});

		it("renders OpenClaw example card", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Quick Start")).toBeInTheDocument();
			});

			// OpenClaw title appears in card header; verify CopyButton exists
			expect(
				screen.getByRole("button", { name: "Copy OpenClaw snippet" }),
			).toBeInTheDocument();
			// Check title exists (may appear multiple times due to code content)
			const openclawTitles = screen.getAllByText("OpenClaw");
			expect(openclawTitles.length).toBeGreaterThan(0);
		});

		it("renders Hermes example card", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Quick Start")).toBeInTheDocument();
			});

			// Hermes title appears in card header; code content has lowercase "hermes"
			// Verify CopyButton exists
			expect(
				screen.getByRole("button", { name: "Copy Hermes snippet" }),
			).toBeInTheDocument();
			// Check title exists (may appear multiple times due to code content)
			const hermesTitles = screen.getAllByText("Hermes");
			expect(hermesTitles.length).toBeGreaterThan(0);
		});

		it("renders LibreChat example card", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Quick Start")).toBeInTheDocument();
			});

			expect(
				screen.getByRole("button", { name: "Copy LibreChat snippet" }),
			).toBeInTheDocument();
			const libreChatTitles = screen.getAllByText("LibreChat");
			expect(libreChatTitles.length).toBeGreaterThan(0);
		});

		it("renders ZED example card", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Quick Start")).toBeInTheDocument();
			});

			expect(
				screen.getByRole("button", { name: "Copy ZED snippet" }),
			).toBeInTheDocument();
			const zedTitles = screen.getAllByText("ZED");
			expect(zedTitles.length).toBeGreaterThan(0);
		});

		it("renders OpenCode example card", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Quick Start")).toBeInTheDocument();
			});

			expect(
				screen.getByRole("button", { name: "Copy OpenCode snippet" }),
			).toBeInTheDocument();
			const opencodeTitles = screen.getAllByText("OpenCode");
			expect(opencodeTitles.length).toBeGreaterThan(0);
		});
	});

	describe("CopyablePill in header", () => {
		it("displays proxy URL that can be copied", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("1 Virtual Key")).toBeInTheDocument();
			});

			// The proxy URL CopyablePill should be present with its tooltip
			expect(
				screen.getByRole("button", { name: "Click to copy proxy URL" }),
			).toBeInTheDocument();
		});
	});
});

describe("Rate Limit RPS = 0", () => {
	it("renders '0' not 'Global' when rate_limit_rps is 0", async () => {
		const keyWithZeroRps = {
			...mockVirtualKey,
			id: "vk-zero-rps",
			name: "Zero RPS Key",
			rate_limit_rps: 0,
			rate_limit_burst: 0,
		};
		server.use(
			http.get("/api/virtual-keys", () => HttpResponse.json([keyWithZeroRps])),
		);

		renderWithProviders(<VirtualKeys />);

		await waitFor(() => {
			expect(screen.getByText("Zero RPS Key")).toBeInTheDocument();
		});

		// Table should show "0" for RPS and Burst, not "Global"
		const table = screen.getByRole("table");
		// Both RPS and Burst are 0
		expect(within(table).getAllByText("0")).toHaveLength(2);
		// "Global" should not appear (null → "Global", but 0 → "0")
		expect(within(table).queryByText("Global")).not.toBeInTheDocument();
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

		// Clear name
		const nameInput = within(dialog).getByLabelText("Name");
		await user.clear(nameInput);

		// Try to save
		await user.click(
			within(dialog).getByRole("button", { name: "Save Changes" }),
		);

		await waitFor(() => {
			expect(putCalled).toBe(false);
		});
		expect(putCalled).toBe(false);
	});

	it("does not call update when name is whitespace only", async () => {
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

		// Clear name and type whitespace
		const nameInput = within(dialog).getByLabelText("Name");
		await user.clear(nameInput);
		await user.type(nameInput, "   ");

		// Try to save
		await user.click(
			within(dialog).getByRole("button", { name: "Save Changes" }),
		);

		await waitFor(() => {
			expect(putCalled).toBe(false);
		});
		expect(putCalled).toBe(false);
	});

	it("sends null for rate_limit_rps when field is cleared", async () => {
		let capturedBody: unknown;
		server.use(
			http.get("/api/virtual-keys", () => HttpResponse.json([mockVirtualKey])),
			http.put("/api/virtual-keys/vk-001", async ({ request }) => {
				capturedBody = await request.json();
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

		// Clear RPS field
		const rpsInput = within(dialog).getByLabelText(
			"Rate Limit RPS (requests/sec)",
		);
		await user.clear(rpsInput);

		// Save
		await user.click(
			within(dialog).getByRole("button", { name: "Save Changes" }),
		);

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
			http.get("/api/virtual-keys", () => HttpResponse.json([mockVirtualKey])),
			http.put("/api/virtual-keys/vk-001", async ({ request }) => {
				capturedBody = await request.json();
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

		// Clear Burst field
		const burstInput = within(dialog).getByLabelText(
			"Rate Limit Burst (max concurrent)",
		);
		await user.clear(burstInput);

		// Save
		await user.click(
			within(dialog).getByRole("button", { name: "Save Changes" }),
		);

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

		// Save should be disabled
		expect(
			within(dialog).getByRole("button", { name: "Save Changes" }),
		).toBeDisabled();
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

	it("closes modal when confirming discard of unsaved changes", async () => {
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

		// Modal should close (user confirmed)
		await waitFor(() => {
			expect(
				screen.queryByRole("dialog", { name: "Virtual Key Details" }),
			).not.toBeInTheDocument();
		});

		confirmSpy.mockRestore();
	});

	it("does not prompt when closing without unsaved changes", async () => {
		server.use(
			http.get("/api/virtual-keys", () => HttpResponse.json([mockVirtualKey])),
		);

		const confirmSpy = vi.spyOn(window, "confirm");

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

describe("Sort resets page", () => {
	it("resets to page 1 when sorting from page 2", async () => {
		const keys = Array.from({ length: 25 }, (_, i) => ({
			...mockVirtualKey,
			id: `vk-${String(i + 1).padStart(3, "0")}`,
			name: `Key ${String(i + 1).padStart(2, "0")}`,
		}));
		server.use(http.get("/api/virtual-keys", () => HttpResponse.json(keys)));

		const { user } = renderWithProviders(<VirtualKeys />);

		await waitFor(() => {
			expect(screen.getByText("25 Virtual Keys")).toBeInTheDocument();
		});

		// Navigate to page 2
		await user.click(screen.getByRole("button", { name: "Next" }));
		await waitFor(() => {
			expect(screen.getByText("11 to 20 of 25 keys")).toBeInTheDocument();
		});

		// Sort by tokens (different column than default name sort)
		const tokensHeader = screen.getByRole("button", {
			name: "Sort by Tokens",
		});
		await user.click(tokensHeader);

		// Should reset to page 1
		await waitFor(() => {
			expect(screen.getByText("1 to 10 of 25 keys")).toBeInTheDocument();
		});
	});
});

describe("API Error Handling", () => {
	it("handles 401 unauthorized error gracefully", async () => {
		server.use(
			http.get("/api/virtual-keys", () =>
				HttpResponse.json({ error: "Unauthorized" }, { status: 401 }),
			),
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
		server.use(
			http.get("/api/virtual-keys", () =>
				HttpResponse.json({ error: "Internal Server Error" }, { status: 500 }),
			),
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
		server.use(http.get("/api/virtual-keys", () => HttpResponse.error()));

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
		server.use(
			http.get("/api/virtual-keys", () => HttpResponse.json([mockVirtualKey])),
		);

		renderWithProviders(<VirtualKeys />);

		await waitFor(() => {
			expect(screen.getByText("Test API Key")).toBeInTheDocument();
		});

		// Table should have proper headers
		expect(
			screen.getByRole("button", { name: "Sort by Name" }),
		).toBeInTheDocument();
		expect(screen.getByText("Key")).toBeInTheDocument();
		expect(
			screen.getByRole("button", { name: "Sort by Created" }),
		).toBeInTheDocument();
		expect(
			screen.getByRole("button", { name: "Sort by Tokens" }),
		).toBeInTheDocument();
		expect(
			screen.getByRole("button", { name: "Sort by Last Used" }),
		).toBeInTheDocument();
	});

	it("has accessible row buttons", async () => {
		server.use(
			http.get("/api/virtual-keys", () => HttpResponse.json([mockVirtualKey])),
		);

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
		server.use(
			http.get("/api/virtual-keys", () => HttpResponse.json([mockVirtualKey])),
		);

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

describe("VirtualKeys edge cases", () => {
	beforeEach(() => {
		server.resetHandlers();
		vi.clearAllMocks();
	});

	it("handles key with null last_used_at", async () => {
		const keyWithNullLastUsed = {
			...mockVirtualKey,
			id: "vk-null-last",
			name: "Never Used Key",
			last_used_at: null,
		};
		server.use(
			http.get("/api/virtual-keys", () =>
				HttpResponse.json([keyWithNullLastUsed]),
			),
		);

		renderWithProviders(<VirtualKeys />);

		await waitFor(() => {
			expect(screen.getByText("Never Used Key")).toBeInTheDocument();
		});
		expect(screen.getByText("Never")).toBeInTheDocument();
	});

	it("handles key with null rate limits", async () => {
		const keyWithNullLimits = {
			...mockVirtualKey,
			id: "vk-null-limits",
			name: "No Limits Key",
			rate_limit_rps: null,
			rate_limit_burst: null,
		};
		server.use(
			http.get("/api/virtual-keys", () =>
				HttpResponse.json([keyWithNullLimits]),
			),
		);

		const { user } = renderWithProviders(<VirtualKeys />);

		await waitFor(() => {
			expect(screen.getByText("No Limits Key")).toBeInTheDocument();
		});

		// Click on the name cell to open detail modal (row click)
		const nameCell = screen.getByText("No Limits Key");
		await user.click(nameCell);

		await waitFor(() => {
			expect(
				screen.getByRole("dialog", { name: "Virtual Key Details" }),
			).toBeInTheDocument();
		});

		const dialog = screen.getByRole("dialog", {
			name: "Virtual Key Details",
		});

		// Verify view mode shows "Global" for null limits (RPS and Burst)
		expect(within(dialog).getAllByText("Global")).toHaveLength(2);

		// Click Edit button to enter edit mode
		const editButton = within(dialog).getByRole("button", {
			name: "Edit",
		});
		await user.click(editButton);

		const rateLimitRpsInput = within(dialog).getByLabelText(
			"Rate Limit RPS (requests/sec)",
		);
		// For number inputs with empty value, use attribute check
		expect(rateLimitRpsInput).toHaveAttribute("value", "");

		const rateLimitBurstInput = within(dialog).getByLabelText(
			"Rate Limit Burst (max concurrent)",
		);
		expect(rateLimitBurstInput).toHaveAttribute("value", "");
	});

	it("creates key with custom rate limits", async () => {
		const newKey = {
			...mockVirtualKey,
			id: "vk-custom",
			name: "Custom Limits Key",
			key: "sk_test_custom_limits",
			key_preview: "sk_test_custom••••",
			rate_limit_rps: 50,
			rate_limit_burst: 100,
		};

		server.use(
			http.get("/api/virtual-keys", () => HttpResponse.json([])),
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
			expect(
				screen.getByText(
					"No virtual keys. Create one to start using the proxy.",
				),
			).toBeInTheDocument();
		});

		const createButton = screen.getByRole("button", {
			name: "+ Create Key",
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
		await user.type(nameInput, "Custom Limits Key");

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
			expect(screen.getByText("sk_test_custom_limits")).toBeInTheDocument();
		});
	});
});
