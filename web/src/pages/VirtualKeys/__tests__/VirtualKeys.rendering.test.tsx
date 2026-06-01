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
				expect(screen.getByText("Virtual Keys")).toBeInTheDocument();
			});
			expect(
				screen.getByRole("button", { name: "Create Key" }),
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
				expect(screen.getByText("Virtual Keys")).toBeInTheDocument();
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
					screen.getByRole("button", { name: "Create Key" }),
				).toBeInTheDocument();
			});
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
				expect(screen.getByText("Virtual Keys")).toBeInTheDocument();
			});

			// The proxy URL CopyablePill should be present with its tooltip
			expect(
				screen.getByRole("button", { name: "Click to copy proxy URL" }),
			).toBeInTheDocument();
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
