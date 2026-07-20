import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import App from "../App";
import { server } from "../test/mocks/server";
import { renderWithProviders } from "../test/utils";

// Mirror App.test.tsx's client mock, adding the totp block so the LoginScreen
// status probe + login flow can be driven per-test. Auth is cookie-derived.
vi.mock("../api/client", () => ({
	isAuthenticated: vi.fn(() => /mh_csrf=[^;\s]/.test(document.cookie)),
	API_BASE: "",
	api: {
		auth: {
			adminExchange: vi.fn().mockResolvedValue({ success: true }),
		},
		settings: {
			get: vi.fn().mockResolvedValue({ app_version: "v0.0.0-test" }),
		},
		version: {
			getLatest: vi.fn().mockResolvedValue({ tag_name: "v0.0.0-test" }),
		},
		publicConfig: {
			get: vi.fn().mockResolvedValue({ read_only: false }),
		},
		demoLogin: {
			get: vi.fn().mockResolvedValue({ token: "" }),
		},
		// canUsePasskeyLogin is not part of api; App imports it from utils/webauthn.
		// Passkey button stays hidden because canUsePasskeyLogin resolves false
		// (jsdom has no WebAuthn). totp.status/login are overridden per-test below.
		totp: {
			status: vi.fn().mockResolvedValue({ enabled: false }),
			login: vi.fn().mockResolvedValue({ token: "session-token-from-server" }),
		},
		oidc: {
			status: vi.fn().mockResolvedValue({ enabled: false }),
		},
		github: {
			status: vi.fn().mockResolvedValue({ enabled: false }),
		},
	},
}));

describe("LoginScreen TOTP step", () => {
	beforeEach(() => {
		localStorage.clear();
		document.cookie = "mh_csrf=; path=/; max-age=0"; // logged out -> LoginScreen
		vi.clearAllMocks();
		server.resetHandlers();
		// Default: status disabled. Tests that need enabled override via
		// vi.mocked(api.totp.status).mockResolvedValue({ enabled: true }).
	});

	it("shows TOTP code input when status is enabled", async () => {
		const { api } = await import("../api/client");
		vi.mocked(api.totp.status).mockResolvedValue({ enabled: true });

		renderWithProviders(<App />);

		expect(await screen.findByLabelText("TOTP code")).toBeInTheDocument();
	});

	it("hides TOTP code input when status is disabled", async () => {
		const { api } = await import("../api/client");
		vi.mocked(api.totp.status).mockResolvedValue({ enabled: false });

		renderWithProviders(<App />);

		// Wait for the sign-in button (status probe settled) then assert
		// the TOTP label is absent.
		await screen.findByRole("button", { name: "Sign In" });
		expect(screen.queryByLabelText("TOTP code")).not.toBeInTheDocument();
	});

	it("submits token+code to totp.login on the TOTP step", async () => {
		const { api } = await import("../api/client");
		vi.mocked(api.totp.status).mockResolvedValue({ enabled: true });
		vi.mocked(api.totp.login).mockResolvedValue({
			token: "ses_sessionTokenValue123",
		});

		const user = userEvent.setup();
		// jsdom's window.location.reload is a no-op for navigation (setup.ts
		// suppresses the "Not implemented" warning). The server sets the session
		// cookie pair; we assert only that the api call fired.

		renderWithProviders(<App />);

		const tokenInput = await screen.findByLabelText("Admin Token");
		await user.type(tokenInput, "raw-admin-token");

		const codeInput = await screen.findByLabelText("TOTP code");
		await user.type(codeInput, "654321");

		const signInBtn = screen.getByRole("button", { name: "Sign In" });
		await user.click(signInBtn);

		await waitFor(() => {
			expect(api.totp.login).toHaveBeenCalledWith("raw-admin-token", "654321");
		});
	});

	it("shows generic totpFailed error on login failure", async () => {
		const { api } = await import("../api/client");
		vi.mocked(api.totp.status).mockResolvedValue({ enabled: true });
		// login rejects -> caught by handleLogin's catch -> setError(totpFailed)
		vi.mocked(api.totp.login).mockRejectedValue(new Error("401"));

		const user = userEvent.setup();

		renderWithProviders(<App />);

		const tokenInput = await screen.findByLabelText("Admin Token");
		await user.type(tokenInput, "raw-admin-token");

		const codeInput = await screen.findByLabelText("TOTP code");
		await user.type(codeInput, "000000");

		const signInBtn = screen.getByRole("button", { name: "Sign In" });
		await user.click(signInBtn);

		// The error div is not role=alert; assert on the message text.
		expect(
			await screen.findByText("Invalid admin token or TOTP code"),
		).toBeInTheDocument();
	});

	it("shows the throttled message on a 429 login response", async () => {
		const { api } = await import("../api/client");
		vi.mocked(api.totp.status).mockResolvedValue({ enabled: true });
		// 429 -> distinct throttled message, not the generic totpFailed.
		vi.mocked(api.totp.login).mockRejectedValue(
			Object.assign(new Error("429 too many"), { status: 429 }),
		);

		const user = userEvent.setup();

		renderWithProviders(<App />);

		const tokenInput = await screen.findByLabelText("Admin Token");
		await user.type(tokenInput, "raw-admin-token");

		const codeInput = await screen.findByLabelText("TOTP code");
		await user.type(codeInput, "000000");

		const signInBtn = screen.getByRole("button", { name: "Sign In" });
		await user.click(signInBtn);

		expect(
			await screen.findByText(
				"Too many attempts. Please wait a moment and try again.",
			),
		).toBeInTheDocument();
	});

	it("shows the SSO button only when oidc status is enabled", async () => {
		const { api } = await import("../api/client");
		vi.mocked(api.oidc.status).mockResolvedValue({
			enabled: true,
			display_name: "auth.example.com",
		});

		renderWithProviders(<App />);

		const sso = await screen.findByTestId("sso-login-button");
		// Button label includes the provider display name and links to /start.
		expect(sso).toHaveTextContent("auth.example.com");
		expect(sso).toHaveAttribute("href", "/api/auth/oidc/start");
	});

	it("hides the SSO button when oidc is disabled", async () => {
		const { api } = await import("../api/client");
		vi.mocked(api.oidc.status).mockResolvedValue({ enabled: false });

		renderWithProviders(<App />);

		await screen.findByRole("button", { name: "Sign In" });
		expect(screen.queryByTestId("sso-login-button")).not.toBeInTheDocument();
	});

	it("shows the GitHub button only when github status is enabled", async () => {
		const { api } = await import("../api/client");
		vi.mocked(api.github.status).mockResolvedValue({ enabled: true });

		renderWithProviders(<App />);

		const gh = await screen.findByTestId("github-login-button");
		expect(gh).toHaveTextContent("GitHub");
		expect(gh).toHaveAttribute("href", "/api/auth/github/start");
	});

	it("hides the GitHub button when github is disabled", async () => {
		const { api } = await import("../api/client");
		vi.mocked(api.github.status).mockResolvedValue({ enabled: false });

		renderWithProviders(<App />);

		await screen.findByRole("button", { name: "Sign In" });
		expect(screen.queryByTestId("github-login-button")).not.toBeInTheDocument();
	});

	it("surfaces an SSO error from the callback fragment and scrubs it", async () => {
		const { api } = await import("../api/client");
		vi.mocked(api.oidc.status).mockResolvedValue({ enabled: false });
		window.location.hash = "#oidc_error=failed";

		renderWithProviders(<App />);

		expect(
			await screen.findByText(/single sign-on failed/i),
		).toBeInTheDocument();
		// Fragment scrubbed so a refresh doesn't re-trigger the error.
		expect(window.location.hash).toBe("");
	});

	it("accepts a full recovery code in the TOTP field (not capped at 6)", async () => {
		const { api } = await import("../api/client");
		vi.mocked(api.totp.status).mockResolvedValue({ enabled: true });

		const user = userEvent.setup();
		renderWithProviders(<App />);

		const codeInput = (await screen.findByLabelText(
			"TOTP code",
		)) as HTMLInputElement;
		const recovery = "ABCD-EFGH-IJKL-MNOP"; // 19-char recovery code
		await user.type(codeInput, recovery);
		// maxLength must allow a recovery code, not truncate to 6 TOTP digits.
		expect(codeInput.value).toBe(recovery);
	});
});
