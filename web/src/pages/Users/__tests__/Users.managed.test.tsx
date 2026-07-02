import { screen, waitFor, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { DashboardUser } from "../../../api/types";
import { mockSystemStats } from "../../../test/mocks/data";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { Users } from "../index";

const mockUser: DashboardUser = {
	id: "11111111-2222-4333-8444-555555555555",
	username: "alice",
	display_name: "Alice A",
	email: "alice@example.com",
	role: "user",
	grants: ["chat", "logs"],
	enabled: true,
	created_at: "2026-07-01T10:00:00Z",
	updated_at: "2026-07-01T10:00:00Z",
	last_login_at: null,
};

const systemWithFleet = (state: "primary" | "member") => ({
	...mockSystemStats,
	fleet: { state, is_primary: state === "primary" },
});

function mockUsersApi(users: DashboardUser[]) {
	server.use(
		http.get("/api/users", () => HttpResponse.json(users)),
		http.get("/api/users/grants", () =>
			HttpResponse.json({
				grants: ["chat", "usage", "logs", "models", "virtual_keys"],
			}),
		),
	);
}

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
		mockUsersApi([mockUser]);
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
		mockUsersApi([mockUser]);
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
		expect(
			within(dialog).queryByTestId("user-reset-password"),
		).not.toBeInTheDocument();
	});

	it("keeps the create button and shows no banner when this instance is the primary", async () => {
		server.use(
			http.get("/api/system", () =>
				HttpResponse.json(systemWithFleet("primary")),
			),
		);
		mockUsersApi([mockUser]);
		renderWithProviders(<Users />);

		expect(await screen.findByTestId("add-user-button")).toBeInTheDocument();
		expect(screen.queryByTestId("managed-banner")).not.toBeInTheDocument();
	});
});
