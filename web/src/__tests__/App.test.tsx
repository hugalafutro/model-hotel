import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { lazy, Suspense } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import App from "../App";
import { api } from "../api/client";
import { mockAllDefaults } from "../test/helpers";
import { server } from "../test/mocks/server";
import { renderWithProviders } from "../test/utils";

// The dashboard is authenticated purely from the readable mh_csrf cookie now.
// Drive login state by setting/clearing that cookie in tests.
function clearAuthCookie() {
	document.cookie = "mh_csrf=; path=/; max-age=0";
}

vi.mock("../api/client", () => ({
	API_BASE: "",
	// Cookie-derived auth signal so AppContent gates on it, mirroring production.
	isAuthenticated: vi.fn(() => /mh_csrf=[^;\s]/.test(document.cookie)),
	api: {
		auth: {
			// Admin-token bootstrap for non-TOTP login.
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
		oidc: {
			status: vi.fn().mockResolvedValue({ enabled: false }),
		},
		github: {
			status: vi.fn().mockResolvedValue({ enabled: false }),
		},
	},
}));

describe("LoginScreen", () => {
	beforeEach(() => {
		localStorage.clear();
		clearAuthCookie(); // logged-out: no session cookie -> LoginScreen renders
		vi.clearAllMocks();
		vi.mocked(api.auth.adminExchange).mockResolvedValue({ success: true });
	});

	it("renders logo, token input, and sign-in button", () => {
		renderWithProviders(<App />);

		expect(screen.getByLabelText("Admin Token")).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Sign In" })).toBeInTheDocument();
		// Login screen renders icons (logo + password-toggle eye) as SVGs
		const logo = document.querySelector("svg");
		expect(logo).toBeInTheDocument();
	});

	it("shows error message when submitting empty token", async () => {
		const user = userEvent.setup();
		renderWithProviders(<App />);

		const signInButton = screen.getByRole("button", { name: "Sign In" });
		await user.click(signInButton);

		expect(screen.getByText("Please enter an admin token")).toBeInTheDocument();
	});

	it("shows error when token is only whitespace", async () => {
		const user = userEvent.setup();
		renderWithProviders(<App />);

		const input = screen.getByLabelText("Admin Token");
		await user.type(input, "   ");

		const signInButton = screen.getByRole("button", { name: "Sign In" });
		await user.click(signInButton);

		expect(screen.getByText("Please enter an admin token")).toBeInTheDocument();
	});

	it("toggles token visibility when eye button clicked", async () => {
		const user = userEvent.setup();
		renderWithProviders(<App />);

		const input = screen.getByLabelText("Admin Token");
		expect(input).toHaveAttribute("type", "password");

		// Click show button (Eye icon)
		const showButton = screen.getByLabelText("Show token");
		await user.click(showButton);

		expect(input).toHaveAttribute("type", "text");
		expect(screen.getByLabelText("Hide token")).toBeInTheDocument();

		// Click hide button (EyeOff icon)
		const hideButton = screen.getByLabelText("Hide token");
		await user.click(hideButton);

		expect(input).toHaveAttribute("type", "password");
	});

	it("exchanges the admin token and reloads on successful submit", async () => {
		const user = userEvent.setup();
		renderWithProviders(<App />);

		const input = screen.getByLabelText("Admin Token");
		await user.type(input, "test-admin-token");

		const signInButton = screen.getByRole("button", { name: "Sign In" });
		await user.click(signInButton);

		// The server sets the session cookie pair; the client just POSTs the token.
		await waitFor(() => {
			expect(api.auth.adminExchange).toHaveBeenCalledWith("test-admin-token");
		});
	});

	it("shows the demo token as a copyable pill, not a second login button", async () => {
		vi.mocked(api.demoLogin.get).mockResolvedValue({ token: "demo-secret" });

		renderWithProviders(<App />);

		const box = await screen.findByTestId("demo-login-box");
		// Token is shown for copying.
		expect(box).toHaveTextContent("demo-secret");
		// The old one-click "log in to the demo" button is gone (the pill copies),
		// and nothing logs in just by rendering the box.
		expect(within(box).queryByRole("button", { name: /log in/i })).toBeNull();
		expect(api.auth.adminExchange).not.toHaveBeenCalled();
	});

	it("does not show the demo token box when no demo token is present", async () => {
		vi.mocked(api.demoLogin.get).mockResolvedValue({ token: "" });
		renderWithProviders(<App />);

		// The sign-in form renders, but the demo box does not.
		expect(
			await screen.findByRole("button", { name: "Sign In" }),
		).toBeInTheDocument();
		expect(screen.queryByTestId("demo-login-box")).not.toBeInTheDocument();
	});

	it("submits on Enter key press", async () => {
		const user = userEvent.setup();
		renderWithProviders(<App />);

		const input = screen.getByLabelText("Admin Token");
		await user.type(input, "test-admin-token{enter}");

		await waitFor(() => {
			expect(api.auth.adminExchange).toHaveBeenCalledWith("test-admin-token");
		});
	});

	it("trims whitespace from token before validating", async () => {
		const user = userEvent.setup();
		renderWithProviders(<App />);

		const input = screen.getByLabelText("Admin Token");
		await user.type(input, "  test-admin-token  ");

		const signInButton = screen.getByRole("button", { name: "Sign In" });
		await user.click(signInButton);

		await waitFor(() => {
			expect(api.auth.adminExchange).toHaveBeenCalledWith("test-admin-token");
		});
	});

	it("shows error when token is invalid (401)", async () => {
		vi.mocked(api.auth.adminExchange).mockRejectedValue(
			Object.assign(new Error("401 Unauthorized"), { status: 401 }),
		);

		const user = userEvent.setup();
		renderWithProviders(<App />);

		const input = screen.getByLabelText("Admin Token");
		await user.type(input, "wrong-token");

		const signInButton = screen.getByRole("button", { name: "Sign In" });
		await user.click(signInButton);

		await waitFor(() => {
			expect(screen.getByText("Invalid admin token")).toBeInTheDocument();
		});
	});

	it("reveals the TOTP field when the admin account has 2FA enabled (400)", async () => {
		// A 400 on the admin-token exchange means the account has TOTP enabled;
		// the admin token alone is not a sufficient credential, so the UI must
		// flip into the TOTP step rather than reporting a hard failure.
		vi.mocked(api.auth.adminExchange).mockRejectedValue(
			Object.assign(new Error("400 Bad Request"), { status: 400 }),
		);

		const user = userEvent.setup();
		renderWithProviders(<App />);

		const input = screen.getByLabelText("Admin Token");
		await user.type(input, "test-admin-token");

		const signInButton = screen.getByRole("button", { name: "Sign In" });
		await user.click(signInButton);

		await waitFor(() => {
			expect(
				screen.getByText("Please enter your TOTP code"),
			).toBeInTheDocument();
		});
		expect(screen.getByLabelText("TOTP code")).toBeInTheDocument();
	});

	it("shows error when server is unreachable", async () => {
		vi.mocked(api.auth.adminExchange).mockRejectedValue(
			new TypeError("Failed to fetch"),
		);

		const user = userEvent.setup();
		renderWithProviders(<App />);

		const input = screen.getByLabelText("Admin Token");
		await user.type(input, "some-token");

		const signInButton = screen.getByRole("button", { name: "Sign In" });
		await user.click(signInButton);

		await waitFor(() => {
			expect(
				screen.getByText("Failed to connect to server"),
			).toBeInTheDocument();
		});
	});

	it("disables sign-in button while validating", async () => {
		// Never-resolving exchange keeps the button in its loading state.
		vi.mocked(api.auth.adminExchange).mockReturnValue(new Promise(() => {}));

		const user = userEvent.setup();
		renderWithProviders(<App />);

		const input = screen.getByLabelText("Admin Token");
		await user.type(input, "test-admin-token");

		const signInButton = screen.getByRole("button", { name: "Sign In" });
		await user.click(signInButton);

		// Button should show loading text and be disabled
		expect(screen.getByRole("button", { name: "Signing in…" })).toBeDisabled();
	});

	it("clears previous error on new submit attempt", async () => {
		// First attempt: invalid token.
		vi.mocked(api.auth.adminExchange).mockRejectedValueOnce(
			Object.assign(new Error("401 Unauthorized"), { status: 401 }),
		);

		const user = userEvent.setup();
		renderWithProviders(<App />);

		const input = screen.getByLabelText("Admin Token");
		await user.type(input, "wrong-token");

		const signInButton = screen.getByRole("button", { name: "Sign In" });
		await user.click(signInButton);

		await waitFor(() => {
			expect(screen.getByText("Invalid admin token")).toBeInTheDocument();
		});

		// Second attempt succeeds (default mock resolves).
		await user.clear(input);
		await user.type(input, "valid-token");
		await user.click(signInButton);

		// Error should be gone (page reloads on success, but at minimum the error clears)
		await waitFor(() => {
			expect(screen.queryByText("Invalid admin token")).not.toBeInTheDocument();
		});
	});

	it("does not submit on Enter while already validating", async () => {
		let callCount = 0;
		vi.mocked(api.auth.adminExchange).mockImplementation(async () => {
			callCount++;
			await new Promise((r) => setTimeout(r, 500));
			return { success: true };
		});

		const user = userEvent.setup();
		renderWithProviders(<App />);

		const input = screen.getByLabelText("Admin Token");
		await user.type(input, "test-admin-token");

		const signInButton = screen.getByRole("button", { name: "Sign In" });
		await user.click(signInButton);

		// Now in loading state — pressing Enter should not trigger another request
		await user.type(input, "{enter}");

		await waitFor(() => {
			expect(callCount).toBe(1);
		});
	});
});

describe("AppContent", () => {
	beforeEach(() => {
		localStorage.clear();
		clearAuthCookie();
		vi.clearAllMocks();
	});

	it("renders LoginScreen when no session cookie is present", () => {
		renderWithProviders(<App />);

		expect(screen.getByLabelText("Admin Token")).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Sign In" })).toBeInTheDocument();
	});

	it("renders Layout with routes when session cookie present", async () => {
		document.cookie = "mh_csrf=existing-token; path=/";

		renderWithProviders(<App />);

		// Layout should render with sidebar navigation
		expect(screen.getByText("Dashboard")).toBeInTheDocument();
		expect(screen.getByText("Providers")).toBeInTheDocument();
		expect(screen.getByText("Models")).toBeInTheDocument();
		expect(screen.getByText("Failover")).toBeInTheDocument();
		expect(screen.getByText("Virtual Keys")).toBeInTheDocument();
		expect(screen.getByText("Logs")).toBeInTheDocument();
		expect(screen.getByText("Settings")).toBeInTheDocument();

		// Flush Layout's background data hooks so their state updates settle
		// inside act() (avoids "not wrapped in act" warnings under CI timing).
		await act(async () => {});
	});

	it("navigates to dashboard by default when session cookie present", async () => {
		document.cookie = "mh_csrf=test-token; path=/";

		renderWithProviders(<App />);

		// Should be on dashboard route
		expect(window.location.pathname).toBe("/");
		// Dashboard content should be visible
		expect(screen.getByText("Dashboard")).toBeInTheDocument();

		await act(async () => {});
	});
});

describe("App providers", () => {
	beforeEach(() => {
		localStorage.clear();
		clearAuthCookie();
	});

	it("wraps AppContent in all required providers", () => {
		renderWithProviders(<App />);

		// If providers are working, context-dependent features should work
		// ThemeProvider: theme classes should be present
		expect(document.documentElement).toHaveClass("dark");

		// StorageProvider: useStorage should work (tested via context tests)
		// ToastProvider: toasts should work
		// EventProvider: SSE should work
		// QuotaModalProvider: quota modal should work
		// SidebarModeProvider: sidebar mode should work

		// Basic sanity check - app renders without crashing
		expect(screen.getByLabelText("Admin Token")).toBeInTheDocument();
	});
});

describe("PageSuspense pattern (Suspense with spinner fallback)", () => {
	beforeEach(() => {
		localStorage.clear();
	});

	it("renders children when resolved", () => {
		renderWithProviders(
			<Suspense
				fallback={
					<div className="flex items-center justify-center h-64">
						<div className="animate-spin rounded-full h-12 w-12 border-b-2 border-(--accent)"></div>
					</div>
				}
			>
				<div data-testid="child-content">Test Content</div>
			</Suspense>,
		);

		expect(screen.getByTestId("child-content")).toBeInTheDocument();
		expect(screen.getByText("Test Content")).toBeInTheDocument();
	});

	it("shows loading spinner fallback for lazy components", async () => {
		// Create a lazy component using React.lazy with a deliberate delay
		// to ensure the spinner fallback is visible before resolution.
		let resolveLazy!: (value: { default: React.ComponentType }) => void;
		const LazyComponent = lazy(
			() =>
				new Promise<{ default: React.ComponentType }>((resolve) => {
					resolveLazy = resolve;
				}),
		);

		const { container } = renderWithProviders(
			<Suspense
				fallback={
					<div className="flex items-center justify-center h-64">
						<div className="animate-spin rounded-full h-12 w-12 border-b-2 border-(--accent)"></div>
					</div>
				}
			>
				<LazyComponent />
			</Suspense>,
		);

		// Initially should show spinner
		const spinner = container.querySelector(".animate-spin");
		expect(spinner).toBeInTheDocument();

		// Manually resolve the lazy component to remove timing dependency
		resolveLazy({
			default: () => <div data-testid="lazy-content">Lazy Loaded</div>,
		});

		// Wait for lazy component to resolve
		await waitFor(
			() => {
				expect(screen.getByTestId("lazy-content")).toBeInTheDocument();
			},
			{ timeout: 10000 },
		);

		expect(screen.getByText("Lazy Loaded")).toBeInTheDocument();
	});
});

describe("Route navigation", () => {
	beforeEach(() => {
		localStorage.clear();
		clearAuthCookie();
		vi.clearAllMocks();
		server.resetHandlers();
		server.use(...mockAllDefaults());
	});

	it("navigates to Logs page", async () => {
		document.cookie = "mh_csrf=test-token; path=/";
		renderWithProviders(<App />);

		const logsLink = screen.getByRole("link", { name: /Requests/ });
		await userEvent.click(logsLink);

		await waitFor(() => {
			expect(
				screen.getByText("Monitor API requests across all providers and keys"),
			).toBeInTheDocument();
		});
	});

	it("navigates to Settings page", async () => {
		document.cookie = "mh_csrf=test-token; path=/";
		renderWithProviders(<App />);

		const settingsLink = screen.getByRole("link", { name: "Settings" });
		await userEvent.click(settingsLink);

		await waitFor(() => {
			expect(
				screen.getByText("Configure your Model Hotel instance"),
			).toBeInTheDocument();
		});
	});

	it("navigates to Virtual Keys page", async () => {
		document.cookie = "mh_csrf=test-token; path=/";
		renderWithProviders(<App />);

		const virtualKeysLink = screen.getByRole("link", { name: "Virtual Keys" });
		await userEvent.click(virtualKeysLink);

		await waitFor(() => {
			expect(
				screen.getByText(/Issue keys for clients to access the proxy at/),
			).toBeInTheDocument();
		});
	});

	it("navigates to Chat page", async () => {
		document.cookie = "mh_csrf=test-token; path=/";
		renderWithProviders(<App />);

		const chatLink = screen.getByRole("link", { name: /Chat/ });
		await userEvent.click(chatLink);

		await waitFor(() => {
			expect(
				screen.getByText("Test enabled models in temporary chat"),
			).toBeInTheDocument();
		});
	});

	it("navigates to Arena page", async () => {
		document.cookie = "mh_csrf=test-token; path=/";
		renderWithProviders(<App />);

		const arenaLink = screen.getByRole("link", { name: /Arena/ });
		await userEvent.click(arenaLink);

		await waitFor(() => {
			expect(
				screen.getByText("Bracket tournament - models compete head-to-head"),
			).toBeInTheDocument();
		});
	});

	it("navigates to Dashboard page", async () => {
		document.cookie = "mh_csrf=test-token; path=/";
		renderWithProviders(<App />);

		// Navigate away first (to Settings), since Dashboard is the default route
		const settingsLink = screen.getByRole("link", { name: "Settings" });
		await userEvent.click(settingsLink);
		await waitFor(() => {
			expect(
				screen.getByText("Configure your Model Hotel instance"),
			).toBeInTheDocument();
		});

		// Now click Dashboard to verify navigation back works
		const dashboardLink = screen.getByRole("link", { name: "Dashboard" });
		await userEvent.click(dashboardLink);

		// Lazy route chunk + render can exceed the default 1s under coverage
		// instrumentation (see the 10s precedent above).
		await waitFor(
			() => {
				expect(
					screen.getByText("Overview of your Model Hotel usage"),
				).toBeInTheDocument();
			},
			{ timeout: 10000 },
		);
	});

	it("navigates to Providers page", async () => {
		document.cookie = "mh_csrf=test-token; path=/";
		renderWithProviders(<App />);

		const providersLink = screen.getByRole("link", { name: "Providers" });
		await userEvent.click(providersLink);

		// Lazy route chunk + identity resolution can exceed the default 1s under
		// coverage instrumentation (see the 10s precedent above).
		await waitFor(
			() => {
				expect(
					screen.getByText("Manage your provider configurations"),
				).toBeInTheDocument();
			},
			{ timeout: 10000 },
		);
	});

	it("navigates to Models page", async () => {
		document.cookie = "mh_csrf=test-token; path=/";
		renderWithProviders(<App />);

		const modelsLink = screen.getByRole("link", { name: "Models" });
		await userEvent.click(modelsLink);

		await waitFor(() => {
			expect(
				screen.getByText("Discovered models from your providers"),
			).toBeInTheDocument();
		});
	});

	it("navigates to Failover Groups page", async () => {
		document.cookie = "mh_csrf=test-token; path=/";
		renderWithProviders(<App />);

		const failoverLink = screen.getByRole("link", { name: "Failover" });
		await userEvent.click(failoverLink);

		await waitFor(() => {
			expect(
				screen.getByText(
					/Route requests through multiple providers in priority order via/,
				),
			).toBeInTheDocument();
		});
	});
});
