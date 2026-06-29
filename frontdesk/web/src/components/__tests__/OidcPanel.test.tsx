import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { expect, it } from "vitest";
import type { Settings } from "../../api/types";
import { ToastProvider } from "../../context/ToastContext";
import { server } from "../../test/server";
import { OidcPanel } from "../OidcPanel";

// MASK matches the server's masked-secret sentinel (alertMaskValue).
const MASK = "********";

function makeSettings(over: Partial<Settings> = {}): Settings {
	return {
		health_poll_secs: 5,
		traefik_poll_secs: 5,
		traefik_stale_secs: 30,
		event_retention_days: 90,
		retry_attempts: 2,
		session_idle_timeout_minutes: 60,
		alert_enabled: false,
		alert_apprise_api_url: "",
		alert_apprise_targets: "",
		alert_events: "",
		oidc_enabled: true,
		oidc_issuer_url: "https://auth.example.com",
		oidc_client_id: "frontdesk",
		oidc_client_secret: MASK, // a secret is stored; served masked
		oidc_public_base_url: "https://frontdesk.example.com",
		oidc_allowed_emails: "admin@example.com",
		...over,
	};
}

// mountWithSettings serves the given settings on GET and captures the next PUT
// body so a test can assert exactly which fields the panel persists.
function mountWithSettings(s: Settings): { puts: Partial<Settings>[] } {
	const puts: Partial<Settings>[] = [];
	server.use(
		http.get("/api/settings", () => HttpResponse.json(s)),
		http.get("/api/auth/oidc/status", () =>
			HttpResponse.json({
				enabled: s.oidc_enabled,
				display_name: "auth.example.com",
			}),
		),
		http.put("/api/settings", async ({ request }) => {
			puts.push((await request.json()) as Partial<Settings>);
			return new HttpResponse(null, { status: 204 });
		}),
	);
	render(
		<ToastProvider>
			<OidcPanel />
		</ToastProvider>,
	);
	return { puts };
}

it("populates fields and derives the redirect URI from the base URL", async () => {
	mountWithSettings(makeSettings());

	expect(
		await screen.findByDisplayValue("https://auth.example.com"),
	).toBeTruthy();
	expect(screen.getByDisplayValue("frontdesk")).toBeTruthy();
	expect(screen.getByDisplayValue("admin@example.com")).toBeTruthy();
	// Redirect URI is built from the public base URL.
	expect(screen.getByTestId("fd-oidc-redirect-uri")).toHaveValue(
		"https://frontdesk.example.com/api/auth/oidc/callback",
	);
});

it("preserves the masked secret and trims edited fields on save", async () => {
	const { puts } = mountWithSettings(makeSettings());

	const clientId = await screen.findByDisplayValue("frontdesk");
	await userEvent.clear(clientId);
	await userEvent.type(clientId, "  frontdesk-2  ");
	await userEvent.click(screen.getByTestId("fd-oidc-save"));

	await waitFor(() => expect(puts).toHaveLength(1));
	const body = puts[0];
	// The untouched, stored secret is sent back as the mask (preserve), not blanked.
	expect(body.oidc_client_secret).toBe(MASK);
	// Edited fields are trimmed.
	expect(body.oidc_client_id).toBe("frontdesk-2");
	expect(body.oidc_enabled).toBe(true);
	expect(body.oidc_issuer_url).toBe("https://auth.example.com");
});

it("collapses the config fields when SSO is disabled", async () => {
	mountWithSettings(makeSettings({ oidc_enabled: false }));

	// The enable checkbox always renders; the issuer field only when enabled.
	await screen.findByTestId("fd-oidc-enable");
	expect(screen.queryByText(/Issuer URL/i)).toBeNull();
});
