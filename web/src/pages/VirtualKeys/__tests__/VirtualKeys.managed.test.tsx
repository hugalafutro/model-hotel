import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
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

	it("shows a read-only note and no actions inside the detail modal for a member", async () => {
		const user = userEvent.setup();
		server.use(
			http.get("/api/system", () =>
				HttpResponse.json(systemWithFleet("member")),
			),
			http.get("/api/virtual-keys", () => HttpResponse.json([mockVirtualKey])),
		);
		renderWithProviders(<VirtualKeys />);

		await user.click(await screen.findByText("Test API Key"));
		const dialog = await screen.findByRole("dialog", {
			name: "Virtual Key Details",
		});
		expect(within(dialog).getByTestId("managed-note")).toBeInTheDocument();
		expect(
			within(dialog).queryByRole("button", { name: "Edit" }),
		).not.toBeInTheDocument();
		expect(
			within(dialog).queryByRole("button", { name: "Delete" }),
		).not.toBeInTheDocument();
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
