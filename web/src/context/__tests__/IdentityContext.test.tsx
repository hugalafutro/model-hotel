import { screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { IdentityProvider, useIdentity } from "../IdentityContext";

function Probe() {
	const { me, isAdmin, can, isLoading } = useIdentity();
	if (isLoading) return <div data-testid="identity-loading" />;
	return (
		<div>
			<span data-testid="username">{me?.username ?? "none"}</span>
			<span data-testid="is-admin">{String(isAdmin)}</span>
			<span data-testid="can-chat">{String(can("chat"))}</span>
			<span data-testid="can-logs">{String(can("logs"))}</span>
		</div>
	);
}

describe("IdentityContext", () => {
	beforeEach(() => {
		server.resetHandlers();
	});

	it("resolves a grant-limited user: can() only for held grants", async () => {
		server.use(
			http.get("/api/auth/me", () =>
				HttpResponse.json({
					username: "carol",
					role: "user",
					grants: ["chat"],
				}),
			),
		);
		renderWithProviders(
			<IdentityProvider>
				<Probe />
			</IdentityProvider>,
		);

		await waitFor(() => {
			expect(screen.getByTestId("username")).toHaveTextContent("carol");
		});
		expect(screen.getByTestId("is-admin")).toHaveTextContent("false");
		expect(screen.getByTestId("can-chat")).toHaveTextContent("true");
		expect(screen.getByTestId("can-logs")).toHaveTextContent("false");
	});

	it("treats an admin role as passing every grant", async () => {
		server.use(
			http.get("/api/auth/me", () =>
				HttpResponse.json({ username: "admin", role: "admin", grants: [] }),
			),
		);
		renderWithProviders(
			<IdentityProvider>
				<Probe />
			</IdentityProvider>,
		);

		await waitFor(() => {
			expect(screen.getByTestId("is-admin")).toHaveTextContent("true");
		});
		expect(screen.getByTestId("can-logs")).toHaveTextContent("true");
	});

	it("falls back to the admin view on a fetch error (server still enforces)", async () => {
		server.use(
			http.get("/api/auth/me", () =>
				HttpResponse.json({ error: "boom" }, { status: 500 }),
			),
		);
		renderWithProviders(
			<IdentityProvider>
				<Probe />
			</IdentityProvider>,
		);

		// The query retries once with backoff before erroring, so allow extra time.
		await waitFor(
			() => {
				expect(screen.getByTestId("is-admin")).toHaveTextContent("true");
			},
			{ timeout: 5000 },
		);
		expect(screen.getByTestId("username")).toHaveTextContent("none");
	});

	it("defaults to the admin view outside a provider", () => {
		renderWithProviders(<Probe />);
		expect(screen.getByTestId("is-admin")).toHaveTextContent("true");
		expect(screen.getByTestId("can-chat")).toHaveTextContent("true");
	});
});
