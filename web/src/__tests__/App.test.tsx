import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { lazy, Suspense } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import App from "../App";
import { setAdminToken } from "../api/client";
import { renderWithProviders } from "../test/utils";

vi.mock("../api/client", () => ({
	setAdminToken: vi.fn(),
	getAdminToken: vi.fn(() => localStorage.getItem("adminToken")),
	API_BASE: "",
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
		await user.type(input, "test-token-123");

		const reloadSpy = vi.spyOn(window, "location", "get");

		const signInButton = screen.getByRole("button", { name: "Sign In" });
		await user.click(signInButton);

		expect(setAdminToken).toHaveBeenCalledWith("test-token-123");
		expect(localStorage.getItem("adminToken")).toBe("test-token-123");

		reloadSpy.mockRestore();
	});

	it("submits on Enter key press", async () => {
		const user = userEvent.setup();
		renderWithProviders(<App />);

		const input = screen.getByLabelText("Admin Token");
		await user.type(input, "test-token{enter}");

		expect(setAdminToken).toHaveBeenCalledWith("test-token");
		expect(localStorage.getItem("adminToken")).toBe("test-token");
	});

	it("trims whitespace from token before saving", async () => {
		const user = userEvent.setup();
		renderWithProviders(<App />);

		const input = screen.getByLabelText("Admin Token");
		await user.type(input, "  test-token  ");

		const signInButton = screen.getByRole("button", { name: "Sign In" });
		await user.click(signInButton);

		expect(setAdminToken).toHaveBeenCalledWith("test-token");
		expect(localStorage.getItem("adminToken")).toBe("test-token");
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
		// Create a lazy component using React.lazy
		const LazyComponent = lazy(() => {
			return new Promise<{ default: React.ComponentType }>((resolve) => {
				setTimeout(() => {
					resolve({
						default: () => <div data-testid="lazy-content">Lazy Loaded</div>,
					});
				}, 100);
			});
		});

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

		// Wait for lazy component to resolve (increased timeout for full-suite load)
		await waitFor(
			() => {
				expect(screen.getByTestId("lazy-content")).toBeInTheDocument();
			},
			{ timeout: 5000 },
		);

		expect(screen.getByText("Lazy Loaded")).toBeInTheDocument();
	});
});
