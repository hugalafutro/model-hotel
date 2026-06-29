import { screen } from "@testing-library/react";
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
});
