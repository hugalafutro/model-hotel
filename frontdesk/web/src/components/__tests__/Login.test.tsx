import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { getAuthToken } from "../../api/client";
import { server } from "../../test/server";
import { Login } from "../Login";

// Mock the WebAuthn browser ceremony so the passkey path is testable headless.
const startAuthentication = vi.fn();
vi.mock("@simplewebauthn/browser", () => ({
	startAuthentication: (...args: unknown[]) => startAuthentication(...args),
}));

beforeEach(() => {
	startAuthentication.mockReset();
	// Login polls OIDC status on mount; default it to disabled so tests that do
	// not care about SSO need not mock it. Per-test server.use overrides this.
	server.use(
		http.get("/api/auth/oidc/status", () =>
			HttpResponse.json({ enabled: false }),
		),
	);
});

describe("Login", () => {
	it("signs in with token + TOTP code when 2FA is enabled", async () => {
		server.use(
			http.get("/api/totp/status", () => HttpResponse.json({ enabled: true })),
			http.get("/api/webauthn/available", () =>
				HttpResponse.json({ enabled: false }),
			),
			http.post("/api/totp/login", async ({ request }) => {
				const body = (await request.json()) as { token: string; code: string };
				expect(body.token).toBe("tok");
				expect(body.code).toBe("123456");
				return HttpResponse.json({ token: "session-token" });
			}),
		);
		const onAuth = vi.fn();
		render(<Login onAuthenticated={onAuth} />);

		// The code field appears once TOTP status resolves to enabled.
		const code = await screen.findByLabelText(/Authentication code/i);
		await userEvent.type(screen.getByLabelText(/Front Desk token/i), "tok");
		await userEvent.type(code, "123456");
		await userEvent.click(screen.getByRole("button", { name: /Verify/i }));

		await waitFor(() => expect(onAuth).toHaveBeenCalled());
		expect(getAuthToken()).toBe("session-token");
	});

	it("signs in with a passkey", async () => {
		server.use(
			http.get("/api/totp/status", () => HttpResponse.json({ enabled: false })),
			http.get("/api/webauthn/available", () =>
				HttpResponse.json({ enabled: true, has_credentials: true }),
			),
			http.post("/api/webauthn/login/start", () =>
				HttpResponse.json({ session_id: "s1", options: { challenge: "abc" } }),
			),
			http.post("/api/webauthn/login/finish", () =>
				HttpResponse.json({ token: "passkey-session" }),
			),
		);
		startAuthentication.mockResolvedValue({ id: "cred" });
		const onAuth = vi.fn();
		render(<Login onAuthenticated={onAuth} />);

		const passkeyBtn = await screen.findByRole("button", {
			name: /Sign in with a passkey/i,
		});
		await userEvent.click(passkeyBtn);

		await waitFor(() => expect(onAuth).toHaveBeenCalled());
		expect(startAuthentication).toHaveBeenCalledWith({
			optionsJSON: { challenge: "abc" },
		});
		expect(getAuthToken()).toBe("passkey-session");
	});

	it("shows an error on a rejected token (TOTP off)", async () => {
		server.use(
			http.get("/api/totp/status", () => HttpResponse.json({ enabled: false })),
			http.get("/api/webauthn/available", () =>
				HttpResponse.json({ enabled: false }),
			),
			http.get("/api/members", () => new HttpResponse("nope", { status: 401 })),
		);
		render(<Login onAuthenticated={vi.fn()} />);
		await userEvent.type(screen.getByLabelText(/Front Desk token/i), "bad");
		await userEvent.click(screen.getByRole("button", { name: /Sign in/i }));
		expect(await screen.findByRole("alert")).toHaveTextContent(/not accepted/i);
	});

	it("shows the SSO button (with provider name) and links to the start route when OIDC is enabled", async () => {
		server.use(
			http.get("/api/totp/status", () => HttpResponse.json({ enabled: false })),
			http.get("/api/webauthn/available", () =>
				HttpResponse.json({ enabled: false }),
			),
			http.get("/api/auth/oidc/status", () =>
				HttpResponse.json({ enabled: true, display_name: "auth.example.com" }),
			),
		);
		render(<Login onAuthenticated={vi.fn()} />);

		const sso = await screen.findByTestId("fd-sso-login-button");
		expect(sso).toHaveAttribute("href", "/api/auth/oidc/start");
		expect(sso).toHaveTextContent(/auth\.example\.com/);
	});

	it("hides the SSO button when OIDC is disabled", async () => {
		server.use(
			http.get("/api/totp/status", () => HttpResponse.json({ enabled: false })),
			http.get("/api/webauthn/available", () =>
				HttpResponse.json({ enabled: false }),
			),
			// oidc/status defaults to disabled via beforeEach.
		);
		render(<Login onAuthenticated={vi.fn()} />);
		await screen.findByLabelText(/Front Desk token/i);
		await waitFor(() => {
			expect(screen.queryByTestId("fd-sso-login-button")).toBeNull();
		});
	});

	it("hides the passkey button when configured but no passkey is registered", async () => {
		server.use(
			http.get("/api/totp/status", () => HttpResponse.json({ enabled: false })),
			http.get("/api/webauthn/available", () =>
				HttpResponse.json({ enabled: true, has_credentials: false }),
			),
		);
		render(<Login onAuthenticated={vi.fn()} />);
		// The token field renders immediately; the passkey button must never appear.
		await screen.findByLabelText(/Front Desk token/i);
		await waitFor(() => {
			expect(
				screen.queryByRole("button", { name: /Sign in with a passkey/i }),
			).toBeNull();
		});
	});
});
