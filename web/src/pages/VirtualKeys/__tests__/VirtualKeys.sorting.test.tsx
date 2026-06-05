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

	describe("Sorting by rate-limit fields", () => {
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
	});

	describe("Restriction badges", () => {
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
});
