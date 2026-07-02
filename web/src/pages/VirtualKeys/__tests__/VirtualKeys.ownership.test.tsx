import { screen, waitFor, within } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { IdentityProvider } from "../../../context/IdentityContext";
import { mockVirtualKey } from "../../../test/mocks/data";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { VirtualKeys } from "../../VirtualKeys";

const roster = [
	{
		id: "11111111-2222-4333-8444-555555555555",
		username: "alice",
		display_name: "Alice",
		email: null,
		role: "user",
		grants: ["virtual_keys"],
		enabled: true,
		created_at: "2026-07-01T10:00:00Z",
		updated_at: "2026-07-01T10:00:00Z",
		last_login_at: null,
	},
];

describe("VirtualKeys ownership", () => {
	beforeEach(() => {
		server.resetHandlers();
		vi.clearAllMocks();
	});

	it("shows an owner chip on owned keys in the table", async () => {
		server.use(
			http.get("/api/virtual-keys", () =>
				HttpResponse.json([
					{
						...mockVirtualKey,
						owner_user_id: roster[0].id,
						owner_username: "alice",
					},
				]),
			),
		);

		renderWithProviders(
			<IdentityProvider>
				<VirtualKeys />
			</IdentityProvider>,
		);

		expect(await screen.findByTestId("vk-owner-chip")).toHaveTextContent(
			"alice",
		);
	});

	it("admin create modal offers the owner select and sends the owner", async () => {
		let postedBody: Record<string, unknown> | null = null;
		server.use(
			http.get("/api/virtual-keys", () => HttpResponse.json([mockVirtualKey])),
			http.get("/api/users", () => HttpResponse.json(roster)),
			http.post("/api/virtual-keys", async ({ request }) => {
				postedBody = (await request.json()) as Record<string, unknown>;
				return HttpResponse.json({
					...mockVirtualKey,
					id: "vk-owned",
					key: "sk_test_key",
					owner_user_id: roster[0].id,
					owner_username: "alice",
				});
			}),
		);

		const { user } = renderWithProviders(
			<IdentityProvider>
				<VirtualKeys />
			</IdentityProvider>,
		);
		await user.click(await screen.findByRole("button", { name: "Create Key" }));

		const dialog = await screen.findByRole("dialog");
		const select = await screen.findByTestId("vk-owner-select");
		await user.selectOptions(select, roster[0].id);
		await user.type(within(dialog).getByLabelText("Name"), "alice-key");
		await user.click(
			within(dialog).getByRole("button", { name: /Create Key/i }),
		);

		await waitFor(() => {
			expect(postedBody).not.toBeNull();
		});
		expect(
			(postedBody as unknown as Record<string, unknown>).owner_user_id,
		).toBe(roster[0].id);
	});

	it("hides the owner select from non-admin users", async () => {
		server.use(
			http.get("/api/auth/me", () =>
				HttpResponse.json({
					username: "alice",
					role: "user",
					grants: ["virtual_keys"],
				}),
			),
			http.get("/api/virtual-keys", () => HttpResponse.json([mockVirtualKey])),
		);

		const { user } = renderWithProviders(
			<IdentityProvider>
				<VirtualKeys />
			</IdentityProvider>,
		);
		await user.click(await screen.findByRole("button", { name: "Create Key" }));

		await screen.findByRole("dialog");
		// The identity defaults to admin while /api/auth/me is in flight, so
		// wait for the resolved user role to hide the select.
		await waitFor(() => {
			expect(screen.queryByTestId("vk-owner-select")).not.toBeInTheDocument();
		});
	});
});
