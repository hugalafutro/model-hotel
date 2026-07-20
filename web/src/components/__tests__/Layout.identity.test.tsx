import { screen } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { IdentityProvider } from "../../context/IdentityContext";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { Layout } from "../Layout";

// The sidebar logout button doubles as the "who am I" indicator: it shows the
// logged-in user's name while keeping its accessible name as "Logout". These
// tests wrap Layout in a real IdentityProvider (AllProviders omits it) so the
// GET /api/auth/me query actually resolves.
describe("Layout — logged-in identity", () => {
	const children = <div data-testid="main-content">Page Content</div>;

	beforeEach(() => {
		vi.clearAllMocks();
		document.cookie = "mh_csrf=test-csrf; path=/";
	});

	it("labels the logout button with the user's display name", async () => {
		server.use(
			http.get("/api/auth/me", () =>
				HttpResponse.json({
					username: "alice",
					display_name: "Alice Smith",
					role: "user",
					grants: [],
				}),
			),
		);

		renderWithProviders(
			<IdentityProvider>
				<Layout>{children}</Layout>
			</IdentityProvider>,
		);

		// Visible label is the display name; the button's accessible name (and
		// tooltip) stay "Logout" so the action is never ambiguous.
		expect(await screen.findByText("Alice Smith")).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Logout" })).toBeInTheDocument();
	});

	it("falls back to the username when no display name is set", async () => {
		server.use(
			http.get("/api/auth/me", () =>
				HttpResponse.json({ username: "bob", role: "user", grants: [] }),
			),
		);

		renderWithProviders(
			<IdentityProvider>
				<Layout>{children}</Layout>
			</IdentityProvider>,
		);

		expect(await screen.findByText("bob")).toBeInTheDocument();
	});
});
