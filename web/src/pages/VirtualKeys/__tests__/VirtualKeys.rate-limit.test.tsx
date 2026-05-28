import { screen, waitFor, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";
import { mockVirtualKey } from "../../../test/mocks/data";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { VirtualKeys } from "../../VirtualKeys";

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
