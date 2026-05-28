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
});
