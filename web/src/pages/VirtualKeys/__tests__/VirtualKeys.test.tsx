import { screen, waitFor, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { mockVirtualKey } from "../../../test/mocks/data";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { VirtualKeys } from "../../VirtualKeys";

describe("VirtualKeys", () => {
	beforeEach(() => {
		server.resetHandlers();
		vi.clearAllMocks();
	});

	afterEach(() => {
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

		it("renders edit button for each key", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});
			expect(screen.getByRole("button", { name: "Edit" })).toBeInTheDocument();
		});

		it("clicking key name opens detail modal", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

			// Click the key name button in the table
			const table = screen.getByRole("table");
			const keyButton = within(table).getByRole("button", {
				name: "Test API Key",
			});
			await user.click(keyButton);

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
		});

		it("clicking edit button opens edit modal with existing key data", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Test API Key")).toBeInTheDocument();
			});

			const editButton = screen.getByRole("button", { name: "Edit" });
			await user.click(editButton);

			await waitFor(() => {
				expect(
					screen.getByRole("dialog", { name: "Edit Virtual Key" }),
				).toBeInTheDocument();
			});

			const dialog = screen.getByRole("dialog", {
				name: "Edit Virtual Key",
			});
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

		it("edits an existing key successfully", async () => {
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

			const editButton = screen.getByRole("button", { name: "Edit" });
			await user.click(editButton);

			await waitFor(() => {
				expect(
					screen.getByRole("dialog", { name: "Edit Virtual Key" }),
				).toBeInTheDocument();
			});

			const dialog = screen.getByRole("dialog", {
				name: "Edit Virtual Key",
			});
			const nameInput = within(dialog).getByLabelText("Name");
			await user.clear(nameInput);
			await user.type(nameInput, "Updated Key Name");

			const submitButton = within(dialog).getByRole("button", {
				name: "Save Changes",
			});
			await user.click(submitButton);

			await waitFor(() => {
				expect(screen.getByText("Virtual key updated")).toBeInTheDocument();
			});

			await waitFor(() => {
				expect(
					screen.queryByRole("dialog", { name: "Edit Virtual Key" }),
				).not.toBeInTheDocument();
			});
		});

		it("shows error toast when update fails", async () => {
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

			const editButton = screen.getByRole("button", { name: "Edit" });
			await user.click(editButton);

			await waitFor(() => {
				expect(
					screen.getByRole("dialog", { name: "Edit Virtual Key" }),
				).toBeInTheDocument();
			});

			const dialog = screen.getByRole("dialog", {
				name: "Edit Virtual Key",
			});
			const nameInput = within(dialog).getByLabelText("Name");
			await user.clear(nameInput);
			await user.type(nameInput, "Updated Key");

			const submitButton = within(dialog).getByRole("button", {
				name: "Save Changes",
			});
			await user.click(submitButton);

			await waitFor(() => {
				expect(screen.getByText(/Failed:.*Update failed/i)).toBeInTheDocument();
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

			const table = screen.getByRole("table");
			const keyButton = within(table).getByRole("button", {
				name: "Test API Key",
			});
			await user.click(keyButton);

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

			const table = screen.getByRole("table");
			const keyButton = within(table).getByRole("button", {
				name: "Test API Key",
			});
			await user.click(keyButton);

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

			const table = screen.getByRole("table");
			const keyButton = within(table).getByRole("button", {
				name: "Test API Key",
			});
			await user.click(keyButton);

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

			const table = screen.getByRole("table");
			const rows = within(table).getAllByRole("row");
			expect(rows).toHaveLength(4);
			expect(rows[1]).toHaveTextContent("Alpha Key");
			expect(rows[2]).toHaveTextContent("Beta Key");
			expect(rows[3]).toHaveTextContent("Zebra Key");
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
				const table = screen.getByRole("table");
				const rows = within(table).getAllByRole("row");
				expect(rows[1]).toHaveTextContent("Zebra Key");
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
			await user.click(createdHeader);

			await waitFor(() => {
				const table = screen.getByRole("table");
				const rows = within(table).getAllByRole("row");
				expect(rows[1]).toHaveTextContent("Old Key");
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
			await user.click(tokensHeader);

			await waitFor(() => {
				const table = screen.getByRole("table");
				const rows = within(table).getAllByRole("row");
				expect(rows[1]).toHaveTextContent("Low Tokens");
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
			await user.click(lastUsedHeader);

			await waitFor(() => {
				const table = screen.getByRole("table");
				const rows = within(table).getAllByRole("row");
				expect(rows[1]).toHaveTextContent("Old Used");
			});
		});

		it("handles sorting by key field (hits default case)", async () => {
			const keys = [
				{ ...mockVirtualKey, id: "vk-001", name: "Key A" },
				{ ...mockVirtualKey, id: "vk-002", name: "Key B" },
			];
			server.use(http.get("/api/virtual-keys", () => HttpResponse.json(keys)));

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Key A")).toBeInTheDocument();
			});

			const table = screen.getByRole("table");
			const rows = within(table).getAllByRole("row");
			expect(rows).toHaveLength(3);
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

			// Tab buttons contain SVG + text; accessible name may vary by jsdom
			const bashTab = screen.getByRole("button", { name: /bash/i });
			expect(bashTab).toHaveClass("terminal-tab-active");
			const powershellTab = screen.getByRole("button", {
				name: /PowerShell/i,
			});
			expect(powershellTab).toHaveClass("terminal-tab-inactive");
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

			const powershellTab = screen.getByRole("button", {
				name: /PowerShell/i,
			});
			await user.click(powershellTab);

			await waitFor(() => {
				expect(powershellTab).toHaveClass("terminal-tab-active");
			});
			const bashTab = screen.getByRole("button", { name: /bash/i });
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

		const editButton = screen.getByRole("button", { name: "Edit" });
		await user.click(editButton);

		await waitFor(() => {
			expect(
				screen.getByRole("dialog", { name: "Edit Virtual Key" }),
			).toBeInTheDocument();
		});

		const dialog = screen.getByRole("dialog", {
			name: "Edit Virtual Key",
		});
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
