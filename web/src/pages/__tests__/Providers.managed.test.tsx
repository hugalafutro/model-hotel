import { screen, waitFor, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { mockProvider, mockSystemStats } from "../../test/mocks/data";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { Providers } from "../Providers";

const systemWithFleet = (state: "primary" | "member") => ({
	...mockSystemStats,
	fleet: { state, is_primary: state === "primary" },
});

describe("Providers managed (fleet member) mode", () => {
	beforeEach(() => {
		server.resetHandlers();
		vi.clearAllMocks();
	});

	it("hides add, edit and delete and shows the managed banner for a member", async () => {
		server.use(
			http.get("/api/system", () =>
				HttpResponse.json(systemWithFleet("member")),
			),
			http.get("/api/providers", () => HttpResponse.json([mockProvider])),
		);
		renderWithProviders(<Providers />);

		expect(await screen.findByTestId("managed-banner")).toBeInTheDocument();
		const card = (await screen.findByText(mockProvider.name)).closest(
			".ui-card",
		) as HTMLElement;
		await waitFor(() => {
			expect(
				screen.queryByRole("button", { name: /Add Provider/i }),
			).not.toBeInTheDocument();
			expect(
				within(card).queryByRole("button", { name: "Edit" }),
			).not.toBeInTheDocument();
			expect(
				within(card).queryByRole("button", { name: "Delete" }),
			).not.toBeInTheDocument();
		});
	});

	it("keeps add, edit and delete and no banner when this instance is the primary", async () => {
		server.use(
			http.get("/api/system", () =>
				HttpResponse.json(systemWithFleet("primary")),
			),
			http.get("/api/providers", () => HttpResponse.json([mockProvider])),
		);
		renderWithProviders(<Providers />);

		expect(
			await screen.findByRole("button", { name: /Add Provider/i }),
		).toBeInTheDocument();
		expect(screen.queryByTestId("managed-banner")).not.toBeInTheDocument();
		const card = (await screen.findByText(mockProvider.name)).closest(
			".ui-card",
		) as HTMLElement;
		expect(
			within(card).getByRole("button", { name: "Edit" }),
		).toBeInTheDocument();
	});
});
