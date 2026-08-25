import { screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { DashboardUser } from "../../../api/types";
import { mockUsersApi } from "../../../test/helpers";
import { mockDashboardUser } from "../../../test/mocks/data";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { Users } from "../index";

const mockAdmin: DashboardUser = {
	...mockDashboardUser,
	id: "66666666-7777-4888-9999-aaaaaaaaaaaa",
	username: "root",
	display_name: "",
	email: null,
	role: "admin",
	grants: [],
	enabled: false,
};

describe("Users page", () => {
	beforeEach(() => {
		server.resetHandlers();
		vi.clearAllMocks();
	});

	it("renders the empty state when no users exist", async () => {
		mockUsersApi([]);
		renderWithProviders(<Users />);
		await waitFor(() => {
			expect(
				screen.getByText(
					"No users yet. Add one to give someone limited access to this dashboard.",
				),
			).toBeInTheDocument();
		});
	});

	it("renders users with role and status badges plus grant labels", async () => {
		mockUsersApi([mockDashboardUser, mockAdmin]);
		renderWithProviders(<Users />);

		await waitFor(() => {
			expect(screen.getByText("alice")).toBeInTheDocument();
		});
		expect(screen.getByText("root")).toBeInTheDocument();
		expect(screen.getByText("Alice A")).toBeInTheDocument();
		expect(screen.getByText("alice@example.com")).toBeInTheDocument();
		// alice: user role, enabled, chat+logs grants
		expect(screen.getByText("Enabled")).toBeInTheDocument();
		expect(screen.getByText("Chat, Request logs")).toBeInTheDocument();
		// root: admin role, disabled, implicit full access
		expect(screen.getByText("Admin")).toBeInTheDocument();
		expect(screen.getByText("Disabled")).toBeInTheDocument();
		expect(screen.getByText("Everything")).toBeInTheDocument();
	});

	it("opens the create modal from the add button", async () => {
		mockUsersApi([]);
		const { user } = renderWithProviders(<Users />);

		await waitFor(() => {
			expect(screen.getByTestId("add-user-button")).toBeInTheDocument();
		});
		await user.click(screen.getByTestId("add-user-button"));
		expect(screen.getByText("Create user")).toBeInTheDocument();
		// Grant checkboxes come from the backend catalog.
		await waitFor(() => {
			expect(screen.getByTestId("grant-chat")).toBeInTheDocument();
		});
		expect(screen.getByTestId("grant-virtual_keys")).toBeInTheDocument();
	});

	it("opens the edit modal when a row is clicked", async () => {
		mockUsersApi([mockDashboardUser]);
		const { user } = renderWithProviders(<Users />);

		await waitFor(() => {
			expect(screen.getByText("alice")).toBeInTheDocument();
		});
		await user.click(screen.getByText("alice"));
		expect(screen.getByText("Edit user")).toBeInTheDocument();
		expect(screen.getByDisplayValue("alice")).toBeInTheDocument();
		expect(screen.getByTestId("user-modal-delete")).toBeInTheDocument();
	});
});
