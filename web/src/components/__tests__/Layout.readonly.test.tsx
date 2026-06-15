import { screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { Layout } from "../Layout";

describe("Layout read-only banner", () => {
	beforeEach(() => {
		server.resetHandlers();
		vi.clearAllMocks();
	});

	it("shows the banner when the server is read-only", async () => {
		server.use(
			http.get("/api/public-config", () =>
				HttpResponse.json({ read_only: true }),
			),
		);

		renderWithProviders(
			<Layout>
				<div>page content</div>
			</Layout>,
		);

		// Appears once the public-config query resolves to read_only: true.
		await waitFor(() => {
			expect(screen.getByTestId("read-only-banner")).toBeInTheDocument();
		});
	});

	it("does not show the banner when not read-only", async () => {
		// Default MSW handler returns { read_only: false }.
		renderWithProviders(
			<Layout>
				<div>page content</div>
			</Layout>,
		);

		await screen.findByText("page content");
		expect(screen.queryByTestId("read-only-banner")).not.toBeInTheDocument();
	});
});
