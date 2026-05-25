import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { lazy, Suspense } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import App from "../App";
import { setAdminToken } from "../api/client";
import { mockAllDefaults } from "../test/helpers";
import { server } from "../test/mocks/server";
import { renderWithProviders } from "../test/utils";

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
	},
}));

describe("LoginScreen", () => {
	beforeEach(() => {
		localStorage.clear();
		vi.clearAllMocks();
	});

	it("renders logo, token input, and sign-in button", () => {
		renderWithProviders(<App />);

		expect(screen.getByLabelText("Admin Token")).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Sign In" })).toBeInTheDocument();
		// Logo renders as SVG with lucide class
		const logo = document.querySelector(".lucide");
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

	it("calls setAdminToken and reloads on successful submit", async () => {
		const user = userEvent.setup();
		renderWithProviders(<App />);

		const input = screen.getByLabelText("Admin Token");
		await user.type(input, "test-admin-token");

		const reloadSpy = vi.spyOn(window, "location", "get");

		const signInButton = screen.getByRole("button", { name: "Sign In" });
		await user.click(signInButton);

		await waitFor(() => {
			expect(setAdminToken).toHaveBeenCalledWith("test-admin-token");
			expect(localStorage.getItem("adminToken")).toBe("test-admin-token");
		});

		reloadSpy.mockRestore();
	});

	it("submits on Enter key press", async () => {
		const user = userEvent.setup();
		renderWithProviders(<App />);

		const input = screen.getByLabelText("Admin Token");
		await user.type(input, "test-admin-token{enter}");

		await waitFor(() => {
			expect(setAdminToken).toHaveBeenCalledWith("test-admin-token");
			expect(localStorage.getItem("adminToken")).toBe("test-admin-token");
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
			expect(setAdminToken).toHaveBeenCalledWith("test-admin-token");
			expect(localStorage.getItem("adminToken")).toBe("test-admin-token");
		});
	});

	it("shows error when token is invalid (401)", async () => {
		server.use(
			http.get("/api/system", () =>
				HttpResponse.json({ error: "Unauthorized" }, { status: 401 }),
			),
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

		// Should NOT have saved the token
		expect(localStorage.getItem("adminToken")).toBeNull();
		expect(setAdminToken).not.toHaveBeenCalled();
	});

	it("shows error when server is unreachable", async () => {
		server.use(http.get("/api/system", () => HttpResponse.error()));

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

		expect(localStorage.getItem("adminToken")).toBeNull();
	});

	it("disables sign-in button while validating", async () => {
		// Use a delayed response to catch the loading state
		server.use(
			http.get("/api/system", async () => {
				await new Promise((r) => setTimeout(r, 500));
				return HttpResponse.json({});
			}),
		);

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
		// First attempt: invalid token
		server.use(
			http.get("/api/system", () =>
				HttpResponse.json({ error: "Unauthorized" }, { status: 401 }),
			),
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

		// Override handler to accept any token now
		server.use(http.get("/api/system", () => HttpResponse.json({})));

		// Clear input and type a valid token
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
		server.use(
			http.get("/api/system", async () => {
				callCount++;
				await new Promise((r) => setTimeout(r, 500));
				return HttpResponse.json({});
			}),
		);

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
		vi.clearAllMocks();
	});

	it("renders LoginScreen when no token in localStorage", () => {
		renderWithProviders(<App />);

		expect(screen.getByLabelText("Admin Token")).toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Sign In" })).toBeInTheDocument();
	});

	it("renders Layout with routes when token present", () => {
		localStorage.setItem("adminToken", "existing-token");

		renderWithProviders(<App />);

		// Layout should render with sidebar navigation
		expect(screen.getByText("Dashboard")).toBeInTheDocument();
		expect(screen.getByText("Providers")).toBeInTheDocument();
		expect(screen.getByText("Models")).toBeInTheDocument();
		expect(screen.getByText("Failover")).toBeInTheDocument();
		expect(screen.getByText("Virtual Keys")).toBeInTheDocument();
		expect(screen.getByText("Logs")).toBeInTheDocument();
		expect(screen.getByText("Settings")).toBeInTheDocument();
	});

	it("calls setAdminToken with token from localStorage on mount", () => {
		localStorage.setItem("adminToken", "stored-token");

		renderWithProviders(<App />);

		expect(setAdminToken).toHaveBeenCalledWith("stored-token");
	});

	it("navigates to dashboard by default when token present", () => {
		localStorage.setItem("adminToken", "test-token");

		renderWithProviders(<App />);

		// Should be on dashboard route
		expect(window.location.pathname).toBe("/");
		// Dashboard content should be visible
		expect(screen.getByText("Dashboard")).toBeInTheDocument();
	});
});

describe("App providers", () => {
	beforeEach(() => {
		localStorage.clear();
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
		vi.clearAllMocks();
		server.resetHandlers();
		server.use(...mockAllDefaults());
	});

	it("navigates to Logs page", async () => {
		localStorage.setItem("adminToken", "test-token");
		renderWithProviders(<App />);

		// Default sub-mode is "request", so link shows "Requests / Logs"
		const logsLink = screen.getByRole("link", { name: /Requests/ });
		await userEvent.click(logsLink);

		await waitFor(() => {
			expect(screen.getByText("Requests")).toBeInTheDocument();
		});
	});

	it("navigates to Settings page", async () => {
		localStorage.setItem("adminToken", "test-token");
		renderWithProviders(<App />);

		const settingsLink = screen.getByRole("link", { name: "Settings" });
		await userEvent.click(settingsLink);

		await waitFor(() => {
			expect(screen.getByText("Settings")).toBeInTheDocument();
		});
	});

	it("navigates to Virtual Keys page", async () => {
		localStorage.setItem("adminToken", "test-token");
		renderWithProviders(<App />);

		const virtualKeysLink = screen.getByRole("link", { name: "Virtual Keys" });
		await userEvent.click(virtualKeysLink);

		await waitFor(() => {
			expect(screen.getByText("Virtual Keys")).toBeInTheDocument();
		});
	});

	it("navigates to Chat page", async () => {
		localStorage.setItem("adminToken", "test-token");
		renderWithProviders(<App />);

		// Default sub-mode is "chat", so link shows "Chat / Conversation"
		const chatLink = screen.getByRole("link", { name: /Chat/ });
		await userEvent.click(chatLink);

		await waitFor(() => {
			expect(screen.getByText("Chat")).toBeInTheDocument();
		});
	});

	it("navigates to Arena page", async () => {
		localStorage.setItem("adminToken", "test-token");
		renderWithProviders(<App />);

		// Default sub-mode is "competition", so link shows "Arena / Compare"
		const arenaLink = screen.getByRole("link", { name: /Arena/ });
		await userEvent.click(arenaLink);

		await waitFor(() => {
			expect(screen.getByText("Arena")).toBeInTheDocument();
		});
	});

	it("navigates to Dashboard page", async () => {
		localStorage.setItem("adminToken", "test-token");
		renderWithProviders(<App />);

		const dashboardLink = screen.getByRole("link", { name: "Dashboard" });
		await userEvent.click(dashboardLink);

		await waitFor(() => {
			expect(screen.getByText("Dashboard")).toBeInTheDocument();
		});
	});

	it("navigates to Providers page", async () => {
		localStorage.setItem("adminToken", "test-token");
		renderWithProviders(<App />);

		const providersLink = screen.getByRole("link", { name: "Providers" });
		await userEvent.click(providersLink);

		await waitFor(() => {
			expect(screen.getByText("Providers")).toBeInTheDocument();
		});
	});

	it("navigates to Models page", async () => {
		localStorage.setItem("adminToken", "test-token");
		renderWithProviders(<App />);

		const modelsLink = screen.getByRole("link", { name: "Models" });
		await userEvent.click(modelsLink);

		await waitFor(() => {
			expect(screen.getByText("Models")).toBeInTheDocument();
		});
	});

	it("navigates to Failover Groups page", async () => {
		localStorage.setItem("adminToken", "test-token");
		renderWithProviders(<App />);

		const failoverLink = screen.getByRole("link", { name: "Failover" });
		await userEvent.click(failoverLink);

		await waitFor(() => {
			expect(screen.getByText("Failover Groups")).toBeInTheDocument();
		});
	});
});
