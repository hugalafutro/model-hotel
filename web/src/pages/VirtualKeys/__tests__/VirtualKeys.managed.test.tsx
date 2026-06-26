import { screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { mockSystemStats, mockVirtualKey } from "../../../test/mocks/data";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { VirtualKeys } from "../../VirtualKeys";

const systemWithFleet = (state: "primary" | "member") => ({
	...mockSystemStats,
	fleet: { state, is_primary: state === "primary" },
});

describe("VirtualKeys managed (fleet member) mode", () => {
	beforeEach(() => {
		server.resetHandlers();
		vi.clearAllMocks();
	});

	it("hides the create button and shows the managed banner for a member", async () => {
		server.use(
			http.get("/api/system", () =>
				HttpResponse.json(systemWithFleet("member")),
			),
			http.get("/api/virtual-keys", () => HttpResponse.json([mockVirtualKey])),
		);
		renderWithProviders(<VirtualKeys />);

		expect(await screen.findByTestId("managed-banner")).toBeInTheDocument();
		await waitFor(() =>
			expect(
				screen.queryByRole("button", { name: "Create Key" }),
			).not.toBeInTheDocument(),
		);
	});

	it("keeps the create button and no banner when this instance is the primary", async () => {
		server.use(
			http.get("/api/system", () =>
				HttpResponse.json(systemWithFleet("primary")),
			),
			http.get("/api/virtual-keys", () => HttpResponse.json([mockVirtualKey])),
		);
		renderWithProviders(<VirtualKeys />);

		expect(
			await screen.findByRole("button", { name: "Create Key" }),
		).toBeInTheDocument();
		expect(screen.queryByTestId("managed-banner")).not.toBeInTheDocument();
	});
});
