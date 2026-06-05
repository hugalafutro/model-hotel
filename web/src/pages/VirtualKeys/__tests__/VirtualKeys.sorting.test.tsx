import { screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import { mockVirtualKey } from "../../../test/mocks/data";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { VirtualKeys } from "../../VirtualKeys";

describe("VirtualKeys", () => {
	beforeEach(() => {
		server.resetHandlers();
	});

	describe("Sorting by different fields", () => {
		it("should sort by tokens when clicking Tokens header", async () => {
			const keys = [
				{
					...mockVirtualKey,
					id: "vk-gamma",
					name: "Gamma Key",
					tokens_used: 1000,
				},
				{
					...mockVirtualKey,
					id: "vk-alpha",
					name: "Alpha Key",
					tokens_used: 100,
				},
				{
					...mockVirtualKey,
					id: "vk-beta",
					name: "Beta Key",
					tokens_used: 500,
				},
			];
			server.use(http.get("/api/virtual-keys", () => HttpResponse.json(keys)));

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Gamma Key")).toBeInTheDocument();
			});

			const tokensHeader = screen.getByRole("button", {
				name: "Sort by Tokens",
			});
			await user.click(tokensHeader);

			await waitFor(() => {
				const table = screen.getByRole("table");
				const rows = table.querySelectorAll("tbody tr");
				expect(rows[0].querySelector("td")?.textContent).toBe("Alpha Key");
			});
		});

		it("should sort by tokens descending when clicking twice", async () => {
			const keys = [
				{
					...mockVirtualKey,
					id: "vk-alpha",
					name: "Alpha Key",
					tokens_used: 100,
				},
				{
					...mockVirtualKey,
					id: "vk-beta",
					name: "Beta Key",
					tokens_used: 500,
				},
				{
					...mockVirtualKey,
					id: "vk-gamma",
					name: "Gamma Key",
					tokens_used: 1000,
				},
			];
			server.use(http.get("/api/virtual-keys", () => HttpResponse.json(keys)));

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Alpha Key")).toBeInTheDocument();
			});

			const tokensHeader = screen.getByRole("button", {
				name: "Sort by Tokens",
			});
			await user.click(tokensHeader);
			await user.click(tokensHeader);

			await waitFor(() => {
				const table = screen.getByRole("table");
				const rows = table.querySelectorAll("tbody tr");
				expect(rows[0].querySelector("td")?.textContent).toBe("Gamma Key");
			});
		});

		it("should sort by RPS when clicking RPS header", async () => {
			const keys = [
				{
					...mockVirtualKey,
					id: "vk-high",
					name: "High RPS Key",
					rate_limit_rps: 50,
				},
				{
					...mockVirtualKey,
					id: "vk-low",
					name: "Low RPS Key",
					rate_limit_rps: 10,
				},
				{
					...mockVirtualKey,
					id: "vk-mid",
					name: "Mid RPS Key",
					rate_limit_rps: 30,
				},
			];
			server.use(http.get("/api/virtual-keys", () => HttpResponse.json(keys)));

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("High RPS Key")).toBeInTheDocument();
			});

			const rpsHeader = screen.getByRole("button", {
				name: "Sort by RPS",
			});
			await user.click(rpsHeader);

			await waitFor(() => {
				const table = screen.getByRole("table");
				const rows = table.querySelectorAll("tbody tr");
				expect(rows[0].querySelector("td")?.textContent).toBe("Low RPS Key");
			});
		});

		it("should sort by burst when clicking Burst header", async () => {
			const keys = [
				{
					...mockVirtualKey,
					id: "vk-high",
					name: "High Burst Key",
					rate_limit_burst: 100,
				},
				{
					...mockVirtualKey,
					id: "vk-low",
					name: "Low Burst Key",
					rate_limit_burst: 20,
				},
				{
					...mockVirtualKey,
					id: "vk-mid",
					name: "Mid Burst Key",
					rate_limit_burst: 50,
				},
			];
			server.use(http.get("/api/virtual-keys", () => HttpResponse.json(keys)));

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("High Burst Key")).toBeInTheDocument();
			});

			const burstHeader = screen.getByRole("button", {
				name: "Sort by Burst",
			});
			await user.click(burstHeader);

			await waitFor(() => {
				const table = screen.getByRole("table");
				const rows = table.querySelectorAll("tbody tr");
				expect(rows[0].querySelector("td")?.textContent).toBe("Low Burst Key");
			});
		});

		it("should sort by last used when clicking Last Used header", async () => {
			const keys = [
				{
					...mockVirtualKey,
					id: "vk-recent",
					name: "Recent Key",
					last_used_at: "2025-06-03T10:00:00Z",
				},
				{
					...mockVirtualKey,
					id: "vk-old",
					name: "Old Key",
					last_used_at: "2025-06-01T10:00:00Z",
				},
				{
					...mockVirtualKey,
					id: "vk-mid",
					name: "Mid Key",
					last_used_at: "2025-06-02T10:00:00Z",
				},
			];
			server.use(http.get("/api/virtual-keys", () => HttpResponse.json(keys)));

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Recent Key")).toBeInTheDocument();
			});

			const lastUsedHeader = screen.getByRole("button", {
				name: "Sort by Last Used",
			});
			await user.click(lastUsedHeader);

			await waitFor(() => {
				const table = screen.getByRole("table");
				const rows = table.querySelectorAll("tbody tr");
				expect(rows[0].querySelector("td")?.textContent).toBe("Old Key");
			});
		});
	});

	describe("Restriction badges", () => {
		it("should show ShieldCheck badge for allowed_providers", async () => {
			const restrictedKey = {
				...mockVirtualKey,
				id: "vk-restricted",
				name: "Restricted Key",
				allowed_providers: ["openai"],
			};

			server.use(
				http.get("/api/virtual-keys", () => HttpResponse.json([restrictedKey])),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Restricted Key")).toBeInTheDocument();
			});

			// Check for ShieldCheck icon (provider restrictions badge) using title
			const shieldIcon = screen.getByTitle("Provider-restricted key");
			expect(shieldIcon).toBeInTheDocument();
		});

		it("should show Brain icon with strikethrough for strip_reasoning", async () => {
			const restrictedKey = {
				...mockVirtualKey,
				id: "vk-restricted",
				name: "Restricted Key",
				strip_reasoning: true,
			};

			server.use(
				http.get("/api/virtual-keys", () => HttpResponse.json([restrictedKey])),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Restricted Key")).toBeInTheDocument();
			});

			// Check for Brain icon (reasoning stripped badge) using title
			// There are 2 elements with this title (span wrapper + svg icon)
			const brainIcons = screen.getAllByTitle("Reasoning stripped");
			expect(brainIcons.length).toBeGreaterThanOrEqual(1);
		});
	});

	describe("Pagination", () => {
		const manyKeys = Array.from({ length: 25 }, (_, i) => ({
			...mockVirtualKey,
			id: `vk-${String(i + 1).padStart(3, "0")}`,
			name: `Key ${String(i + 1).padStart(3, "0")}`,
			tokens_used: (i + 1) * 100,
			rate_limit_rps: (i + 1) * 2,
			rate_limit_burst: (i + 1) * 5,
			last_used_at: `2025-06-${String(Math.min(i + 1, 30)).padStart(2, "0")}T10:00:00Z`,
			created_at: `2025-06-${String(Math.min(i + 1, 30)).padStart(2, "0")}T00:00:00Z`,
		}));

		it("should change page size via PaginationBar", async () => {
			server.use(
				http.get("/api/virtual-keys", () => HttpResponse.json(manyKeys)),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Virtual Keys")).toBeInTheDocument();
			});

			// The select has no aria-label; find by role
			const pageSizeSelect = screen.getByRole("combobox");
			expect(pageSizeSelect).toHaveValue("10");

			// Change to 20 per page
			await user.selectOptions(pageSizeSelect, "20");

			await waitFor(() => {
				expect(
					screen.getByText("1 to 20 of 25 keys", { exact: true }),
				).toBeInTheDocument();
			});
		});

		it("should navigate to page 2 with enough data", async () => {
			server.use(
				http.get("/api/virtual-keys", () => HttpResponse.json(manyKeys)),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Key 001")).toBeInTheDocument();
			});

			// Click Next to go to page 2
			const nextPageButton = screen.getByRole("button", {
				name: "Next",
			});
			await user.click(nextPageButton);

			await waitFor(() => {
				expect(
					screen.getByText("11 to 20 of 25 keys", { exact: true }),
				).toBeInTheDocument();
				expect(screen.getByText("Key 011")).toBeInTheDocument();
			});
		});
	});
});
