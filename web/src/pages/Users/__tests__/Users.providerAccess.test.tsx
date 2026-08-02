import { screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { DashboardUser, Provider } from "../../../api/types";
import { mockProvider } from "../../../test/mocks/data";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { Users } from "../index";

// Uncapped: no allowed_providers field at all. The field is optional, so
// omitting it (rather than passing null) is also a valid "every provider"
// signal and exercises that path.
const uncappedUser: DashboardUser = {
	id: "11111111-2222-4333-8444-555555555555",
	username: "alice",
	display_name: "Alice A",
	email: "alice@example.com",
	role: "user",
	grants: ["chat"],
	enabled: true,
	created_at: "2026-07-01T10:00:00Z",
	updated_at: "2026-07-01T10:00:00Z",
	last_login_at: null,
};

const singleCappedUser: DashboardUser = {
	...uncappedUser,
	id: "22222222-2222-4333-8444-555555555555",
	username: "bob",
	allowed_providers: ["p1"],
};

const multiCappedUser: DashboardUser = {
	...uncappedUser,
	id: "33333333-2222-4333-8444-555555555555",
	username: "carol",
	allowed_providers: ["p1", "p2", "p3"],
};

const providers: Provider[] = [
	{ ...mockProvider, id: "p1", name: "openai" },
	{ ...mockProvider, id: "p2", name: "anthropic" },
	{ ...mockProvider, id: "p3", name: "groq" },
];

function mockUsersApi(users: DashboardUser[]) {
	server.use(
		http.get("/api/users", () => HttpResponse.json(users)),
		http.get("/api/users/grants", () =>
			HttpResponse.json({ grants: ["chat", "usage", "logs"] }),
		),
		http.get("/api/providers", () => HttpResponse.json(providers)),
	);
}

describe("Users page provider access column", () => {
	beforeEach(() => {
		server.resetHandlers();
		vi.clearAllMocks();
	});

	it("marks an uncapped account as reaching all providers", async () => {
		mockUsersApi([uncappedUser, singleCappedUser, multiCappedUser]);
		renderWithProviders(<Users />);

		await waitFor(() => {
			expect(
				screen.getByTestId(`user-provider-access-${uncappedUser.id}`),
			).toBeInTheDocument();
		});
		const cell = screen.getByTestId(`user-provider-access-${uncappedUser.id}`);
		expect(cell).toHaveAttribute("data-provider-access", "all");
	});

	it("shows the resolved provider name for a single-provider cap", async () => {
		mockUsersApi([uncappedUser, singleCappedUser, multiCappedUser]);
		renderWithProviders(<Users />);

		const cell = await screen.findByTestId(
			`user-provider-access-${singleCappedUser.id}`,
		);
		await waitFor(() => {
			expect(cell).toHaveAttribute("data-provider-access", "selected");
		});
		expect(cell.textContent).toContain("openai");
	});

	it("shows the first provider name plus a remainder count for a multi-provider cap", async () => {
		mockUsersApi([uncappedUser, singleCappedUser, multiCappedUser]);
		renderWithProviders(<Users />);

		const cell = await screen.findByTestId(
			`user-provider-access-${multiCappedUser.id}`,
		);
		await waitFor(() => {
			expect(cell).toHaveAttribute("data-provider-access", "selected");
		});
		expect(cell.textContent).toContain("openai");
		expect(cell.textContent).toContain("2");
	});

	it("renders a raw id when the cap references a deleted provider", async () => {
		const orphanUser: DashboardUser = {
			...uncappedUser,
			id: "44444444-2222-4333-8444-555555555555",
			username: "dave",
			allowed_providers: ["ghost-provider"],
		};
		mockUsersApi([orphanUser]);
		renderWithProviders(<Users />);

		const cell = await screen.findByTestId(
			`user-provider-access-${orphanUser.id}`,
		);
		await waitFor(() => {
			expect(cell).toHaveAttribute("data-provider-access", "selected");
		});
		expect(cell.textContent).toContain("ghost-provider");
	});
});
