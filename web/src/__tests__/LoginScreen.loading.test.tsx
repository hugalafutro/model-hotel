import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import App from "../App";
import { server } from "../test/mocks/server";
import { renderWithProviders } from "../test/utils";

// Mirror LoginScreen.usertotp.test.tsx's client mock; both totp and auth
// blocks are present so either login path can be driven per-test.
vi.mock("../api/client", () => ({
	isAuthenticated: vi.fn(() => /mh_csrf=[^;\s]/.test(document.cookie)),
	API_BASE: "",
	api: {
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
		totp: {
			status: vi.fn().mockResolvedValue({ enabled: false }),
			login: vi.fn(),
		},
		oidc: {
			status: vi.fn().mockResolvedValue({ enabled: false }),
		},
		github: {
			status: vi.fn().mockResolvedValue({ enabled: false }),
		},
		auth: {
			status: vi.fn().mockResolvedValue({ enabled: false }),
			login: vi.fn(),
		},
	},
}));

// canUsePasskeyLogin/loginWithPasskey are mocked so the passkey button can
// render and enter its loading state without a real WebAuthn API (jsdom).
vi.mock("../utils/webauthn", () => ({
	canUsePasskeyLogin: vi.fn().mockResolvedValue(true),
	loginWithPasskey: vi.fn().mockReturnValue(new Promise(() => {})),
}));

describe("LoginScreen loading indicators", () => {
	beforeEach(() => {
		localStorage.clear();
		document.cookie = "mh_csrf=; path=/; max-age=0"; // logged out -> LoginScreen
		vi.clearAllMocks();
		server.resetHandlers();
	});

	it("shows the spinner on the admin-token sign-in button while logging in", async () => {
		const { api } = await import("../api/client");
		vi.mocked(api.totp.status).mockResolvedValue({ enabled: true });
		// Never-resolving login keeps `loading` true so the spinner is visible.
		vi.mocked(api.totp.login).mockReturnValue(new Promise(() => {}));

		const user = userEvent.setup();
		renderWithProviders(<App />);

		// No spinner before submit.
		expect(screen.queryByTestId("spinner")).not.toBeInTheDocument();

		await user.type(
			await screen.findByLabelText("Admin Token"),
			"raw-admin-token",
		);
		await user.type(await screen.findByLabelText("TOTP code"), "654321");

		const signInBtn = screen.getByRole("button", { name: "Sign In" });
		await user.click(signInBtn);

		// The braille spinner renders inside the button during loading.
		expect(await screen.findByTestId("spinner")).toBeInTheDocument();
		expect(signInBtn).toBeDisabled();
		expect(signInBtn).toHaveTextContent("Signing in");
	});

	it("shows the spinner on the username/password sign-in button while logging in", async () => {
		const { api } = await import("../api/client");
		vi.mocked(api.auth.status).mockResolvedValue({ enabled: true });
		// Never-resolving login keeps `userLoading` true.
		vi.mocked(api.auth.login).mockReturnValue(new Promise(() => {}));

		const user = userEvent.setup();
		renderWithProviders(<App />);

		expect(screen.queryByTestId("spinner")).not.toBeInTheDocument();

		await user.type(await screen.findByLabelText("Username"), "alice");
		await user.type(screen.getByLabelText("Password"), "correct-horse");

		const btn = screen.getByTestId("user-login-button");
		await user.click(btn);

		expect(await screen.findByTestId("spinner")).toBeInTheDocument();
		expect(btn).toBeDisabled();
		expect(btn).toHaveTextContent("Signing in");
	});

	it("shows the spinner on the passkey sign-in button while logging in", async () => {
		const user = userEvent.setup();
		renderWithProviders(<App />);

		// The passkey button renders because canUsePasskeyLogin is mocked true.
		const passkeyBtn = await screen.findByRole("button", {
			name: /sign in with passkey/i,
		});

		expect(screen.queryByTestId("spinner")).not.toBeInTheDocument();
		await user.click(passkeyBtn);

		// loginWithPasskey never resolves, keeping passkeyLoading true.
		expect(await screen.findByTestId("spinner")).toBeInTheDocument();
		expect(passkeyBtn).toBeDisabled();
		// Accessible name reflects loading (dynamic aria-label); the decorative
		// spinner glyph does not pollute it (aria-hidden on the Spinner span).
		expect(passkeyBtn).toHaveAccessibleName("Signing in\u2026");
	});
});
