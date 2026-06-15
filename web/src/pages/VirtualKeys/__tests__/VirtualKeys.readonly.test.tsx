import { screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { mockVirtualKey } from "../../../test/mocks/data";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { VirtualKeys } from "../../VirtualKeys";

describe("VirtualKeys read-only mode", () => {
	beforeEach(() => {
		server.resetHandlers();
		vi.clearAllMocks();
	});

	it("hides the create button when the server is read-only", async () => {
		server.use(
			http.get("/api/public-config", () =>
				HttpResponse.json({ read_only: true }),
			),
			http.get("/api/virtual-keys", () => HttpResponse.json([mockVirtualKey])),
		);

		renderWithProviders(<VirtualKeys />);

		await waitFor(() => {
			expect(screen.getByText("Virtual Keys")).toBeInTheDocument();
		});
		// Button starts present (read-only defaults false) and disappears once
		// the public-config query resolves to read_only: true.
		await waitFor(() => {
			expect(
				screen.queryByRole("button", { name: "Create Key" }),
			).not.toBeInTheDocument();
		});
	});

	it("shows the create button when not read-only", async () => {
		server.use(
			http.get("/api/virtual-keys", () => HttpResponse.json([mockVirtualKey])),
		);

		renderWithProviders(<VirtualKeys />);

		await waitFor(() => {
			expect(
				screen.getByRole("button", { name: "Create Key" }),
			).toBeInTheDocument();
		});
	});
});
