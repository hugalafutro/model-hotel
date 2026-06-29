import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { OidcPanel } from "../OidcSettings";

function mockSettings(values: Record<string, string>) {
	server.use(
		http.get("/api/settings", ({ request }) => {
			if (!request.headers.get("Authorization")?.startsWith("Bearer ")) {
				return HttpResponse.json({ error: "Unauthorized" }, { status: 401 });
			}
			return HttpResponse.json(values);
		}),
	);
}

function mockOidcStatus(enabled: boolean) {
	server.use(
		http.get("/api/auth/oidc/status", () =>
			HttpResponse.json({ enabled, display_name: "auth.example.com" }),
		),
	);
}

describe("OidcPanel", () => {
	beforeEach(() => {
		server.resetHandlers();
		vi.clearAllMocks();
	});

	it("hides config inputs when SSO is disabled", async () => {
		mockSettings({ oidc_enabled: "false" });
		renderWithProviders(<OidcPanel />);
		await screen.findByTestId("oidc-panel");
		expect(screen.queryByTestId("oidc-issuer-input")).not.toBeInTheDocument();
		expect(
			screen.queryByTestId("oidc-client-secret-input"),
		).not.toBeInTheDocument();
	});

	it("shows inputs, a configured secret, and the derived redirect URI when enabled", async () => {
		mockSettings({
			oidc_enabled: "true",
			oidc_issuer_url: "https://auth.example.com",
			oidc_client_id: "model-hotel",
			oidc_client_secret: "********",
			oidc_public_base_url: "https://hotel.example.com",
		});
		mockOidcStatus(true);
		renderWithProviders(<OidcPanel />);

		const issuer = (await screen.findByTestId(
			"oidc-issuer-input",
		)) as HTMLInputElement;
		expect(issuer.value).toBe("https://auth.example.com");

		// Secret is configured, so the clear button is shown.
		expect(screen.getByTestId("oidc-client-secret-clear")).toBeInTheDocument();

		// Redirect URI is derived from the public base URL (no trailing slash).
		expect(
			screen.getByText("https://hotel.example.com/api/auth/oidc/callback"),
		).toBeInTheDocument();
	});

	it("derives the redirect URI without a doubled slash", async () => {
		mockSettings({
			oidc_enabled: "true",
			oidc_public_base_url: "https://hotel.example.com/",
		});
		mockOidcStatus(false);
		renderWithProviders(<OidcPanel />);

		expect(
			await screen.findByText(
				"https://hotel.example.com/api/auth/oidc/callback",
			),
		).toBeInTheDocument();
	});

	it("commits the issuer on blur and clears the secret", async () => {
		mockSettings({
			oidc_enabled: "true",
			oidc_issuer_url: "https://auth.example.com",
			oidc_client_secret: "********",
			oidc_public_base_url: "https://hotel.example.com",
		});
		mockOidcStatus(true);
		const puts: Record<string, string>[] = [];
		server.use(
			http.put("/api/settings", async ({ request }) => {
				puts.push((await request.json()) as Record<string, string>);
				return HttpResponse.json({ ok: true });
			}),
		);
		const user = userEvent.setup();
		renderWithProviders(<OidcPanel />);

		// Edit issuer, then blur -> commits oidc_issuer_url via the shared commit().
		const issuer = await screen.findByTestId("oidc-issuer-input");
		await user.clear(issuer);
		await user.type(issuer, "https://idp.test");
		await user.tab();
		await waitFor(() =>
			expect(puts.some((p) => p.oidc_issuer_url === "https://idp.test")).toBe(
				true,
			),
		);

		// Clear the configured secret -> commits an empty oidc_client_secret.
		await user.click(screen.getByTestId("oidc-client-secret-clear"));
		await waitFor(() =>
			expect(puts.some((p) => p.oidc_client_secret === "")).toBe(true),
		);
	});

	it("toggles enable off", async () => {
		mockSettings({ oidc_enabled: "true", oidc_issuer_url: "https://a.test" });
		mockOidcStatus(true);
		const puts: Record<string, string>[] = [];
		server.use(
			http.put("/api/settings", async ({ request }) => {
				puts.push((await request.json()) as Record<string, string>);
				return HttpResponse.json({ ok: true });
			}),
		);
		const user = userEvent.setup();
		renderWithProviders(<OidcPanel />);

		await screen.findByTestId("oidc-panel");
		await user.click(screen.getByRole("switch", { name: "Enable SSO" }));
		await waitFor(() =>
			expect(puts.some((p) => p.oidc_enabled === "false")).toBe(true),
		);
	});
});
