import { screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMockLogEntry, createMockLogs } from "../../test/logFixtures";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { Logs } from "../Logs";

// Admin-only owner filter on the request-logs page: the dropdown renders from
// GET /api/users and its selection rides the list query as owner_user_id.

const usersFixture = [
	{
		id: "11111111-2222-4333-8444-555555555555",
		username: "alice",
		display_name: "Alice",
		email: null,
		role: "user",
		grants: ["logs"],
		enabled: true,
		created_at: "2026-07-01T10:00:00Z",
		updated_at: "2026-07-01T10:00:00Z",
		last_login_at: null,
	},
];

describe("Logs owner filter", () => {
	beforeEach(() => {
		server.resetHandlers();
		vi.clearAllMocks();
		localStorage.clear();
		localStorage.setItem("requestLogsViewMode", "paginate");
	});

	it("filters the list by the selected owner", async () => {
		const ownedLogs = createMockLogs([
			createMockLogEntry({ model_id: "alice-model" }),
		]);
		const allLogs = createMockLogs([
			createMockLogEntry({ model_id: "everyone-model" }),
		]);
		server.use(
			http.get("/api/users", () => HttpResponse.json(usersFixture)),
			http.get("/api/logs", ({ request }) => {
				const owner = new URL(request.url).searchParams.get("owner_user_id");
				return HttpResponse.json(
					owner === usersFixture[0].id ? ownedLogs : allLogs,
				);
			}),
		);

		const { user } = renderWithProviders(<Logs />);

		await waitFor(() => {
			expect(screen.getByText("everyone-model")).toBeInTheDocument();
		});

		// The owner dropdown appears once the roster loads (default identity in
		// tests is admin).
		const ownerButton = await screen.findByRole("button", { name: /Owner/ });
		await user.click(ownerButton);
		await user.click(await screen.findByText("alice"));

		await waitFor(() => {
			expect(screen.getByText("alice-model")).toBeInTheDocument();
		});
		expect(screen.queryByText("everyone-model")).not.toBeInTheDocument();
	});

	it("hides the owner dropdown when the roster is empty", async () => {
		const seen = createMockLogs([
			createMockLogEntry({ model_id: "solo-model" }),
		]);
		server.use(
			http.get("/api/users", () => HttpResponse.json([])),
			http.get("/api/logs", () => HttpResponse.json(seen)),
		);
		renderWithProviders(<Logs />);
		await waitFor(() => {
			expect(screen.getByText("solo-model")).toBeInTheDocument();
		});
		expect(
			screen.queryByRole("button", { name: /Owner/ }),
		).not.toBeInTheDocument();
	});
});
