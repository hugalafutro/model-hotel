import { screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { DashboardUser } from "../../../api/types";
import i18n from "../../../i18n";
import { serveUsersApi } from "../../../test/helpers";
import { mockDashboardUser } from "../../../test/mocks/data";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { Users } from "../index";

// Three accounts chosen so that no two columns produce the same order. Last
// login in particular deliberately does NOT track the username order: bob
// sorts second by name but last by login, so a comparator wired to the wrong
// field cannot pass by accident.
//
//            username   role    enabled   last login
//  carol     3rd        user    yes       middle
//  alice     1st        admin   no        never
//  bob       2nd        user    no        newest
const carol: DashboardUser = {
	...mockDashboardUser,
	id: "aaaaaaaa-1111-4111-8111-111111111111",
	username: "carol",
	role: "user",
	enabled: true,
	last_login_at: "2026-01-05T10:00:00Z",
};
const alice: DashboardUser = {
	...mockDashboardUser,
	id: "bbbbbbbb-2222-4222-8222-222222222222",
	username: "alice",
	role: "admin",
	enabled: false,
	last_login_at: null,
};
const bob: DashboardUser = {
	...mockDashboardUser,
	id: "cccccccc-3333-4333-8333-333333333333",
	username: "bob",
	role: "user",
	enabled: false,
	last_login_at: "2026-08-20T10:00:00Z",
};

/**
 * Usernames in the order the table currently renders them. The body rows carry
 * role="button" (the whole row opens the edit modal), so they are read off the
 * table structure rather than the row role.
 */
function renderedOrder(container: HTMLElement): string[] {
	return Array.from(container.querySelectorAll("tbody tr")).map(
		(row) => row.querySelector("td")?.textContent ?? "",
	);
}

async function sortBy(
	user: ReturnType<typeof renderWithProviders>["user"],
	labelKey: string,
) {
	await user.click(
		screen.getByRole("button", {
			name: i18n.t("components.dataTable.sortBy", {
				label: i18n.t(labelKey),
			}),
		}),
	);
}

describe("Users page sorting", () => {
	beforeEach(() => {
		server.resetHandlers();
		vi.clearAllMocks();
	});

	it("orders by username ascending before anything is clicked", async () => {
		serveUsersApi([carol, alice, bob]);
		const { container } = renderWithProviders(<Users />);
		await waitFor(() => {
			expect(screen.getByText("carol")).toBeInTheDocument();
		});
		expect(renderedOrder(container)).toEqual(["alice", "bob", "carol"]);
	});

	it("sorts by role, and reverses on a second click of the same column", async () => {
		serveUsersApi([carol, alice, bob]);
		const { container, user } = renderWithProviders(<Users />);
		await waitFor(() => {
			expect(screen.getByText("carol")).toBeInTheDocument();
		});

		await sortBy(user, "users.table.role");
		// admin first; the two "user" rows tie, and a stable sort leaves them in
		// the order the API returned them (carol, then bob).
		expect(renderedOrder(container)).toEqual(["alice", "carol", "bob"]);

		await sortBy(user, "users.table.role");
		expect(renderedOrder(container)).toEqual(["carol", "bob", "alice"]);
	});

	it("sorts by status with the disabled accounts first, then reversed", async () => {
		serveUsersApi([carol, alice, bob]);
		const { container, user } = renderWithProviders(<Users />);
		await waitFor(() => {
			expect(screen.getByText("carol")).toBeInTheDocument();
		});

		await sortBy(user, "users.table.status");
		expect(renderedOrder(container)).toEqual(["alice", "bob", "carol"]);

		await sortBy(user, "users.table.status");
		expect(renderedOrder(container)).toEqual(["carol", "alice", "bob"]);
	});

	it("sorts by last login, treating a never-logged-in account as the oldest", async () => {
		serveUsersApi([carol, alice, bob]);
		const { container, user } = renderWithProviders(<Users />);
		await waitFor(() => {
			expect(screen.getByText("carol")).toBeInTheDocument();
		});

		// Oldest first, and the never-logged-in account is the oldest of all.
		// This order differs from every other column's, so it can only come
		// from the last_login arm.
		await sortBy(user, "users.table.lastLogin");
		expect(renderedOrder(container)).toEqual(["alice", "carol", "bob"]);

		await sortBy(user, "users.table.lastLogin");
		expect(renderedOrder(container)).toEqual(["bob", "carol", "alice"]);
	});

	it("starts a newly picked column ascending rather than inheriting the last direction", async () => {
		serveUsersApi([carol, alice, bob]);
		const { container, user } = renderWithProviders(<Users />);
		await waitFor(() => {
			expect(screen.getByText("carol")).toBeInTheDocument();
		});

		// username is already the active ascending column, so one click flips it.
		await sortBy(user, "users.table.username");
		expect(renderedOrder(container)).toEqual(["carol", "bob", "alice"]);

		await sortBy(user, "users.table.lastLogin");
		expect(renderedOrder(container)).toEqual(["alice", "carol", "bob"]);
	});
});
