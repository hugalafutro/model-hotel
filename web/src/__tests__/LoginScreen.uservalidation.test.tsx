import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import App from "../App";
import i18n from "../i18n";
import { server } from "../test/mocks/server";
import { renderWithProviders } from "../test/utils";

// Same client mock as the user-TOTP suite: the username/password form only
// renders when the unauthenticated auth probe reports an enabled user account.
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
			status: vi.fn().mockResolvedValue({ enabled: true }),
			login: vi.fn(),
		},
	},
}));

const totpRequiredError = () =>
	Object.assign(new Error('Login failed: 401 {"totp_required":true}'), {
		status: 401,
	});

// The login form is the one surface an unauthenticated visitor can reach, so
// every way it can go wrong has to say something specific. A blank field must
// not become a network round trip, and a throttled attempt must not read as a
// wrong password: an operator who cannot tell those apart keeps retrying into
// the rate limiter.
describe("LoginScreen user credential errors", () => {
	beforeEach(() => {
		localStorage.clear();
		document.cookie = "mh_csrf=; path=/; max-age=0"; // logged out -> LoginScreen
		vi.clearAllMocks();
		server.resetHandlers();
	});

	async function fields() {
		return {
			username: await screen.findByLabelText(i18n.t("layout.auth.username")),
			password: screen.getByLabelText(i18n.t("layout.auth.password")),
			submit: screen.getByTestId("user-login-button"),
		};
	}

	it("refuses to submit an incomplete form and never calls the API", async () => {
		const { api } = await import("../api/client");
		const user = userEvent.setup();
		renderWithProviders(<App />);

		const f = await fields();
		await user.click(f.submit);
		expect(
			await screen.findByText(i18n.t("layout.auth.userCredentialsRequired")),
		).toBeInTheDocument();
		expect(api.auth.login).not.toHaveBeenCalled();

		// A username alone is still incomplete, and whitespace is not a username.
		await user.type(f.username, "   ");
		await user.click(f.submit);
		expect(
			await screen.findByText(i18n.t("layout.auth.userCredentialsRequired")),
		).toBeInTheDocument();
		expect(api.auth.login).not.toHaveBeenCalled();

		await user.clear(f.username);
		await user.type(f.username, "alice");
		await user.click(f.submit);
		expect(
			await screen.findByText(i18n.t("layout.auth.userCredentialsRequired")),
		).toBeInTheDocument();
		expect(api.auth.login).not.toHaveBeenCalled();
	});

	it("asks for the code once the TOTP step is open rather than resubmitting blank", async () => {
		const { api } = await import("../api/client");
		vi.mocked(api.auth.login).mockRejectedValue(totpRequiredError());
		const user = userEvent.setup();
		renderWithProviders(<App />);

		const f = await fields();
		await user.type(f.username, "alice");
		await user.type(f.password, "correct-horse");
		await user.click(f.submit);
		await screen.findByTestId("user-totp-code");
		expect(api.auth.login).toHaveBeenCalledTimes(1);

		await user.click(screen.getByTestId("user-login-button"));
		expect(
			await screen.findByText(i18n.t("layout.auth.totpCodeRequired")),
		).toBeInTheDocument();
		expect(api.auth.login).toHaveBeenCalledTimes(1);
	});

	it("names throttling separately from a rejected password", async () => {
		const { api } = await import("../api/client");
		vi.mocked(api.auth.login).mockRejectedValue(
			Object.assign(new Error("Login failed: 429 too many attempts"), {
				status: 429,
			}),
		);
		const user = userEvent.setup();
		renderWithProviders(<App />);

		const f = await fields();
		await user.type(f.username, "alice");
		await user.type(f.password, "correct-horse");
		await user.click(f.submit);

		expect(
			await screen.findByText(i18n.t("layout.auth.userLoginThrottled")),
		).toBeInTheDocument();
		expect(
			screen.queryByText(i18n.t("layout.auth.userLoginFailed")),
		).not.toBeInTheDocument();
	});

	it("falls back to the generic failure for anything else", async () => {
		const { api } = await import("../api/client");
		vi.mocked(api.auth.login).mockRejectedValue(
			Object.assign(new Error("Login failed: 500 upstream is down"), {
				status: 500,
			}),
		);
		const user = userEvent.setup();
		renderWithProviders(<App />);

		const f = await fields();
		await user.type(f.username, "alice");
		await user.type(f.password, "hunter2");
		await user.click(f.submit);

		expect(
			await screen.findByText(i18n.t("layout.auth.userLoginFailed")),
		).toBeInTheDocument();
		// The form stays usable rather than getting stuck in its loading state.
		await waitFor(() => {
			expect(screen.getByTestId("user-login-button")).not.toBeDisabled();
		});
	});
});
