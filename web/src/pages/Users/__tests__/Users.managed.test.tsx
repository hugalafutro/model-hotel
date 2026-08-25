import { screen, waitFor, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { mockUsersApi } from "../../../test/helpers";
import { mockDashboardUser, mockSystemStats } from "../../../test/mocks/data";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { Users } from "../index";

const systemWithFleet = (state: "primary" | "member") => ({
	...mockSystemStats,
	fleet: { state, is_primary: state === "primary" },
});

describe("Users managed (fleet member) mode", () => {
	beforeEach(() => {
		server.resetHandlers();
		vi.clearAllMocks();
	});

	it("hides the create button and shows the managed banner for a member", async () => {
		server.use(
			http.get("/api/system", () =>
				HttpResponse.json(systemWithFleet("member")),
			),
		);
		mockUsersApi([mockDashboardUser]);
		renderWithProviders(<Users />);

		expect(await screen.findByTestId("managed-banner")).toBeInTheDocument();
		await waitFor(() =>
			expect(screen.queryByTestId("add-user-button")).not.toBeInTheDocument(),
		);
	});

	it("shows a read-only note and hides save/delete/reset inside the edit modal for a member", async () => {
		server.use(
			http.get("/api/system", () =>
				HttpResponse.json(systemWithFleet("member")),
			),
		);
		mockUsersApi([mockDashboardUser]);
		const { user } = renderWithProviders(<Users />);

		await user.click(await screen.findByText("alice"));
		const dialog = await screen.findByRole("dialog", {
			name: "Edit user",
		});
		expect(within(dialog).getByTestId("managed-note")).toBeInTheDocument();
		expect(
			within(dialog).queryByTestId("user-modal-save"),
		).not.toBeInTheDocument();
		expect(
			within(dialog).queryByTestId("user-modal-delete"),
		).not.toBeInTheDocument();
		// The reset-password field's label ("Reset password") maps to its input
		// via htmlFor, so queryByLabelText actually resolves to the element when
		// present (unlike a testId the field never carried). This makes the
		// absence assertion non-vacuous: it fails if the !managed gate on the
		// reset section is ever dropped.
		expect(
			within(dialog).queryByLabelText("Reset password"),
		).not.toBeInTheDocument();
	});

	it("keeps the create button and shows no banner when this instance is the primary", async () => {
		server.use(
			http.get("/api/system", () =>
				HttpResponse.json(systemWithFleet("primary")),
			),
		);
		mockUsersApi([mockDashboardUser]);
		renderWithProviders(<Users />);

		expect(await screen.findByTestId("add-user-button")).toBeInTheDocument();
		expect(screen.queryByTestId("managed-banner")).not.toBeInTheDocument();
	});
});
