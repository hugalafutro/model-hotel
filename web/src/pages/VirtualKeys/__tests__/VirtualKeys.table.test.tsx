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
					(el) =>
						el.tagName === "TD" ||
						el.parentElement?.tagName === "TD" ||
						el.parentElement?.parentElement?.tagName === "TD",
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
					(el) =>
						el.tagName === "TD" ||
						el.parentElement?.tagName === "TD" ||
						el.parentElement?.parentElement?.tagName === "TD",
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
					(el) =>
						el.tagName === "TD" ||
						el.parentElement?.tagName === "TD" ||
						el.parentElement?.parentElement?.tagName === "TD",
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
				expect(screen.getByText("Virtual Keys")).toBeInTheDocument();
			});

			// Pagination shows "X to Y of Z keys" format (single text node via i18n)
			expect(
				screen.getByText("1 to 10 of 15 keys", { exact: true }),
			).toBeInTheDocument();
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
				expect(screen.getByText("Virtual Keys")).toBeInTheDocument();
			});

			// The select has no aria-label; find by role
			const pageSizeSelect = screen.getByRole("combobox");
			// PaginationBar options: 10, 20, 30, 40, 50
			await user.selectOptions(pageSizeSelect, "20");

			await waitFor(() => {
				expect(
					screen.getByText("1 to 20 of 25 keys", { exact: true }),
				).toBeInTheDocument();
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
				expect(screen.getByText("Virtual Keys")).toBeInTheDocument();
			});

			const nextPageButton = screen.getByRole("button", {
				name: "Next",
			});
			await user.click(nextPageButton);

			await waitFor(() => {
				expect(
					screen.getByText("11 to 15 of 15 keys", { exact: true }),
				).toBeInTheDocument();
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
				expect(screen.getByText("Virtual Keys")).toBeInTheDocument();
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

			expect(
				screen.getByRole("button", { name: "Prev", exact: true }),
			).toBeDisabled();
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
				expect(
					screen.getByRole("button", { name: "Next", exact: true }),
				).toBeDisabled();
			});
		});
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
			expect(screen.getByText("Virtual Keys")).toBeInTheDocument();
		});

		// Navigate to page 2
		await user.click(screen.getByRole("button", { name: "Next" }));
		await waitFor(() => {
			expect(
				screen.getByText("11 to 20 of 25 keys", { exact: true }),
			).toBeInTheDocument();
		});

		// Sort by tokens (different column than default name sort)
		const tokensHeader = screen.getByRole("button", {
			name: "Sort by Tokens",
		});
		await user.click(tokensHeader);

		// Should reset to page 1
		await waitFor(() => {
			expect(
				screen.getByText("1 to 10 of 25 keys", { exact: true }),
			).toBeInTheDocument();
		});
	});
});
