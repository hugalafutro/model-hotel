import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import App from "../App";
import { server } from "../test/mocks/server";
import { renderWithProviders } from "../test/utils";

// Mirror LoginScreen.usertotp.test.tsx's client mock; both totp and auth
// blocks are present so either login path can be driven per-test.
vi.mock("../api/client", () => ({
	setAdminToken: vi.fn(),
	getAdminToken: vi.fn(() => localStorage.getItem("adminToken")),
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

describe("LoginScreen loading indicators", () => {
	beforeEach(() => {
		localStorage.clear();
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
});
