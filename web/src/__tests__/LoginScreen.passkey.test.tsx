import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import App from "../App";
import i18n from "../i18n";
import { server } from "../test/mocks/server";
import { renderWithProviders } from "../test/utils";

// Same client mock as LoginScreen.loading.test.tsx: every login probe reports
// disabled so only the admin-token form and the passkey button render.
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

// The WebAuthn ceremony itself is mocked (jsdom has no authenticator); these
// tests pin what the screen does with each outcome it can report.
vi.mock("../utils/webauthn", () => ({
	canUsePasskeyLogin: vi.fn().mockResolvedValue(true),
	loginWithPasskey: vi.fn(),
}));

describe("LoginScreen passkey sign-in outcomes", () => {
	const reload = vi.fn();
	const original = window.location;

	beforeEach(() => {
		localStorage.clear();
		document.cookie = "mh_csrf=; path=/; max-age=0"; // logged out -> LoginScreen
		vi.clearAllMocks();
		server.resetHandlers();
		Object.defineProperty(window, "location", {
			value: { ...original, reload },
			configurable: true,
		});
	});

	afterEach(() => {
		Object.defineProperty(window, "location", {
			value: original,
			configurable: true,
		});
	});

	async function clickPasskey() {
		const user = userEvent.setup();
		renderWithProviders(<App />);
		const btn = await screen.findByRole("button", {
			name: /sign in with passkey/i,
		});
		await user.click(btn);
		return btn;
	}

	it("reloads into the app once the assertion is accepted", async () => {
		const { loginWithPasskey } = await import("../utils/webauthn");
		vi.mocked(loginWithPasskey).mockResolvedValue(true);

		await clickPasskey();

		await waitFor(() => expect(reload).toHaveBeenCalledTimes(1));
		expect(
			screen.queryByText(i18n.t("layout.auth.passkeyFailed")),
		).not.toBeInTheDocument();
	});

	it("shows the failure and re-enables the button when the assertion is rejected", async () => {
		const { loginWithPasskey } = await import("../utils/webauthn");
		vi.mocked(loginWithPasskey).mockRejectedValue(new Error("rejected"));

		const btn = await clickPasskey();

		expect(
			await screen.findByText(i18n.t("layout.auth.passkeyFailed")),
		).toBeInTheDocument();
		expect(btn).toBeEnabled();
		expect(reload).not.toHaveBeenCalled();
	});

	it("stays on the login screen without an error when the prompt is dismissed", async () => {
		const { loginWithPasskey } = await import("../utils/webauthn");
		vi.mocked(loginWithPasskey).mockResolvedValue(false);

		const btn = await clickPasskey();

		await waitFor(() => expect(btn).toBeEnabled());
		expect(reload).not.toHaveBeenCalled();
		expect(
			screen.queryByText(i18n.t("layout.auth.passkeyFailed")),
		).not.toBeInTheDocument();
	});
});
