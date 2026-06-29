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

	it("commits each editable field, sets and clears the secret", async () => {
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

		await screen.findByTestId("oidc-panel");

		// Each text field commits its own key on blur via the shared commit().
		// A per-field waitFor keeps the refetch from one commit from racing the
		// next field's edit.
		const commitField = async (testid: string, key: string, value: string) => {
			const el = await screen.findByTestId(testid);
			await user.clear(el);
			await user.type(el, value);
			await user.tab();
			await waitFor(() =>
				expect(puts.some((p) => p[key] === value)).toBe(true),
			);
		};

		await commitField(
			"oidc-issuer-input",
			"oidc_issuer_url",
			"https://idp.test",
		);
		await commitField("oidc-client-id-input", "oidc_client_id", "my-client");
		await commitField(
			"oidc-base-url-input",
			"oidc_public_base_url",
			"https://hotel.test",
		);
		await commitField(
			"oidc-allowed-emails-input",
			"oidc_allowed_emails",
			"a@b.test",
		);

		// Setting a new secret commits its value...
		await commitField(
			"oidc-client-secret-input",
			"oidc_client_secret",
			"new-secret",
		);
		// ...and the clear button commits an empty string.
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
