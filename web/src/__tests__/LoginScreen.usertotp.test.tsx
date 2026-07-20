import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import App from "../App";
import { server } from "../test/mocks/server";
import { renderWithProviders } from "../test/utils";

// Mirror LoginScreen.totp.test.tsx's client mock, adding the auth block so the
// username/password form renders and its TOTP second step can be driven. Auth
// is cookie-derived.
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

// The server's 401 {"totp_required": true} surfaces through ApiError's
// message (fetchOK appends the response body).
const totpRequiredError = () =>
	Object.assign(new Error('Login failed: 401 {"totp_required":true}'), {
		status: 401,
	});

describe("LoginScreen user TOTP step", () => {
	beforeEach(() => {
		localStorage.clear();
		document.cookie = "mh_csrf=; path=/; max-age=0"; // logged out -> LoginScreen
		vi.clearAllMocks();
		server.resetHandlers();
	});

	async function submitCredentials(user: ReturnType<typeof userEvent.setup>) {
		await user.type(await screen.findByLabelText("Username"), "alice");
		await user.type(screen.getByLabelText("Password"), "correct-horse");
		await user.click(screen.getByTestId("user-login-button"));
	}

	it("reveals the code field when the server answers totp_required", async () => {
		const { api } = await import("../api/client");
		vi.mocked(api.auth.login).mockRejectedValue(totpRequiredError());

		const user = userEvent.setup();
		renderWithProviders(<App />);

		expect(screen.queryByTestId("user-totp-code")).not.toBeInTheDocument();
		await submitCredentials(user);

		expect(await screen.findByTestId("user-totp-code")).toBeInTheDocument();
		// The first exchange never carries a code.
		expect(api.auth.login).toHaveBeenCalledWith(
			"alice",
			"correct-horse",
			undefined,
		);
	});

	it("resubmits with the code and logs in", async () => {
		const { api } = await import("../api/client");
		vi.mocked(api.auth.login)
			.mockRejectedValueOnce(totpRequiredError())
			.mockResolvedValueOnce({ token: "ses_userSessionToken" });

		const user = userEvent.setup();
		renderWithProviders(<App />);
		await submitCredentials(user);

		const codeInput = await screen.findByTestId("user-totp-code");
		await user.type(codeInput, "123456");
		await user.click(screen.getByTestId("user-login-button"));

		await waitFor(() => {
			expect(api.auth.login).toHaveBeenLastCalledWith(
				"alice",
				"correct-horse",
				"123456",
			);
		});
	});

	it("shows the code error on a wrong code, not the generic login failure", async () => {
		const { api } = await import("../api/client");
		vi.mocked(api.auth.login)
			.mockRejectedValueOnce(totpRequiredError())
			.mockRejectedValueOnce(
				Object.assign(new Error("Login failed: 401 invalid TOTP code"), {
					status: 401,
				}),
			);

		const user = userEvent.setup();
		renderWithProviders(<App />);
		await submitCredentials(user);

		const codeInput = await screen.findByTestId("user-totp-code");
		await user.type(codeInput, "000000");
		await user.click(screen.getByTestId("user-login-button"));

		expect(
			await screen.findByText("Invalid TOTP or recovery code"),
		).toBeInTheDocument();
	});

	it("accepts a full recovery code in the user code field", async () => {
		const { api } = await import("../api/client");
		vi.mocked(api.auth.login).mockRejectedValue(totpRequiredError());

		const user = userEvent.setup();
		renderWithProviders(<App />);
		await submitCredentials(user);

		const codeInput = (await screen.findByTestId(
			"user-totp-code",
		)) as HTMLInputElement;
		const recovery = "ABCD-EFGH-IJKL-MNOP";
		await user.type(codeInput, recovery);
		expect(codeInput.value).toBe(recovery);
	});
});
