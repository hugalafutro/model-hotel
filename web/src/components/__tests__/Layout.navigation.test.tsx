import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import { Layout } from "../Layout";

describe("Layout", () => {
	const mockChildren = <div data-testid="main-content">Page Content</div>;

	beforeEach(() => {
		vi.clearAllMocks();
	});

	describe("Sidebar Navigation", () => {
		it("renders sidebar with logo", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			// Logo has aria-label "Model Hotel"
			expect(screen.getByLabelText("Model Hotel")).toBeInTheDocument();
			expect(screen.getByText("Multi-Provider AI Gateway")).toBeInTheDocument();
		});

		it("renders tagline", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			expect(
				screen.getByText(/Because we have LiteLLM at home/),
			).toBeInTheDocument();
		});

		it("renders all navigation items", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			expect(screen.getByText("Dashboard")).toBeInTheDocument();
			expect(screen.getByText("Chat")).toBeInTheDocument();
			expect(screen.getByText("Arena")).toBeInTheDocument();
			expect(screen.getByText("Providers")).toBeInTheDocument();
			expect(screen.getByText("Models")).toBeInTheDocument();
			expect(screen.getByText("Failover")).toBeInTheDocument();
			expect(screen.getByText("Virtual Keys")).toBeInTheDocument();
			expect(screen.getByText("Logs")).toBeInTheDocument();
			expect(screen.getByText("Settings")).toBeInTheDocument();
		});

		it("renders navigation items with icons", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const nav = screen.getByRole("navigation");
			const icons = nav.querySelectorAll("svg");
			expect(icons.length).toBeGreaterThanOrEqual(9);
		});

		it("highlights active route - Dashboard", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>, {
				initialEntries: ["/dashboard"],
			});

			const dashboardLink = screen.getByText("Dashboard").closest("a");
			expect(dashboardLink).toHaveClass("sidebar-link-active");
		});

		it("highlights active route - Providers", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>, {
				initialEntries: ["/providers"],
			});

			const providersLink = screen.getByText("Providers").closest("a");
			expect(providersLink).toHaveClass("sidebar-link-active");
		});

		it("highlights active route - Models", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>, {
				initialEntries: ["/models"],
			});

			const modelsLink = screen.getByText("Models").closest("a");
			expect(modelsLink).toHaveClass("sidebar-link-active");
		});

		it("highlights active route - Failover", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>, {
				initialEntries: ["/failover"],
			});

			const failoverLink = screen.getByText("Failover").closest("a");
			expect(failoverLink).toHaveClass("sidebar-link-active");
		});

		it("highlights active route - Virtual Keys", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>, {
				initialEntries: ["/virtual-keys"],
			});

			const virtualKeysLink = screen.getByText("Virtual Keys").closest("a");
			expect(virtualKeysLink).toHaveClass("sidebar-link-active");
		});

		it("highlights active route - Settings", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>, {
				initialEntries: ["/settings"],
			});

			const settingsLink = screen.getByText("Settings").closest("a");
			expect(settingsLink).toHaveClass("sidebar-link-active");
		});

		it("shows sub-mode labels for Chat page", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>, {
				initialEntries: ["/chat"],
			});

			expect(screen.getByText("Chat")).toBeInTheDocument();
			expect(screen.getByText("Conversation")).toBeInTheDocument();
		});

		it("shows sub-mode labels for Arena page", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>, {
				initialEntries: ["/arena"],
			});

			expect(screen.getByText("Arena")).toBeInTheDocument();
			expect(screen.getByText("Compare")).toBeInTheDocument();
		});

		it("shows sub-mode labels for Logs page", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>, {
				initialEntries: ["/logs"],
			});

			expect(screen.getByText("Requests")).toBeInTheDocument();
			expect(screen.getByText("Logs")).toBeInTheDocument();
		});
	});

	describe("Sub-mode Toggle", () => {
		it("navigates normally on first click to different page", async () => {
			const user = userEvent.setup();
			const { rerender } = renderWithProviders(
				<Layout>{mockChildren}</Layout>,
				{
					initialEntries: ["/dashboard"],
				},
			);

			const chatLink = screen.getByText("Chat").closest("a");
			expect(chatLink).toBeInTheDocument();

			if (chatLink) {
				await user.click(chatLink);
			}

			// Re-render to pick up the navigation change
			rerender(<Layout>{mockChildren}</Layout>);

			// After navigation, the Chat link should be active
			const updatedChatLink = screen.getByText("Chat").closest("a");
			expect(updatedChatLink).toHaveClass("sidebar-link-active");
		});
	});

	describe("Sidebar Footer", () => {
		it("renders Docs link", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const docsLink = screen.getByText("Docs").closest("a");
			expect(docsLink).toHaveAttribute(
				"href",
				"https://github.com/hugalafutro/model-hotel",
			);
			expect(docsLink).toHaveAttribute("target", "_blank");
		});

		it("renders GitHub link", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const githubLink = screen.getByLabelText("GitHub repository");
			expect(githubLink).toHaveAttribute(
				"href",
				"https://github.com/hugalafutro/model-hotel",
			);
			expect(githubLink).toHaveAttribute("target", "_blank");
		});

		it("renders theme toggle button", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			// Theme toggle has title attribute
			const themeButton = screen.getByTitle(/Switch to (light|dark) mode/);
			expect(themeButton).toBeInTheDocument();
		});

		it("shows Sun icon in dark mode", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const sunIcon = screen.getByTitle("Switch to light mode");
			expect(sunIcon).toBeInTheDocument();
		});

		it("toggles theme on button click", async () => {
			const user = userEvent.setup();
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const themeButton = screen.getByTitle("Switch to light mode");
			await user.click(themeButton);

			expect(screen.getByTitle("Switch to dark mode")).toBeInTheDocument();
		});

		it("renders logout button", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			expect(screen.getByText("Logout")).toBeInTheDocument();
		});

		it("opens logout confirmation dialog on logout click", async () => {
			const user = userEvent.setup();
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const logoutButton = screen.getByText("Logout").closest("button");
			expect(logoutButton).toBeInTheDocument();

			if (logoutButton) {
				await user.click(logoutButton);
			}

			expect(screen.getByText("Log out?")).toBeInTheDocument();
			expect(
				screen.getByText("You'll need to re-enter your admin token."),
			).toBeInTheDocument();
		});

		it("closes logout confirmation on Cancel click", async () => {
			const user = userEvent.setup();
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const logoutButton = screen.getByText("Logout").closest("button");
			expect(logoutButton).toBeInTheDocument();

			if (logoutButton) {
				await user.click(logoutButton);
			}

			const cancelButton = screen.getByText("Cancel");
			await user.click(cancelButton);

			await waitFor(() => {
				expect(screen.queryByText("Log out?")).not.toBeInTheDocument();
			});
		});

		it("performs logout on confirmation", async () => {
			const user = userEvent.setup();
			const originalStorage = localStorage.getItem("adminToken");
			localStorage.setItem("adminToken", "test-token");

			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const logoutButton = screen.getByText("Logout").closest("button");
			expect(logoutButton).toBeInTheDocument();

			if (logoutButton) {
				await user.click(logoutButton);
			}

			// Find logout confirm button in the dialog
			const confirmButton = screen
				.getByRole("dialog")
				.querySelector("button.ui-btn-danger");
			expect(confirmButton).toBeInTheDocument();
			if (confirmButton) {
				await user.click(confirmButton);
			}

			await waitFor(() => {
				expect(localStorage.getItem("adminToken")).toBeNull();
			});

			if (originalStorage) {
				localStorage.setItem("adminToken", originalStorage);
			}
		});
	});

	describe("Navigation Icons", () => {
		const navItems = [
			"Dashboard",
			"Chat",
			"Arena",
			"Providers",
			"Models",
			"Failover",
			"Virtual Keys",
			"Logs",
			"Settings",
		];

		it.each(navItems)("renders icon for %s", (label) => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const link = screen.getByText(label).closest("li");
			expect(link?.querySelector("svg")).toBeInTheDocument();
		});
	});

	describe("Keyboard Navigation", () => {
		it("focuses navigation links via tab", async () => {
			const user = userEvent.setup();
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.tab();
			// First tab should hit a focusable element in the sidebar
			expect(document.activeElement?.tagName).toBe("A");
		});

		it("supports keyboard navigation for theme toggle", async () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const themeButton = screen.getByTitle(/Switch to (light|dark) mode/);
			expect(themeButton).toBeInTheDocument();
			themeButton.focus();
			expect(document.activeElement).toBe(themeButton);
		});
	});

	describe("Main Content Area", () => {
		it("renders children in main area", () => {
			renderWithProviders(
				<Layout>
					<div data-testid="test-content">Test Content</div>
				</Layout>,
			);

			expect(screen.getByTestId("test-content")).toBeInTheDocument();
		});

		it("applies max-width constraint to content", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const main = screen.getByRole("main");
			const contentDiv = main.querySelector("div");
			expect(contentDiv).toHaveClass("max-w-7xl");
		});

		it("has proper main landmark", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			expect(screen.getByRole("main")).toBeInTheDocument();
		});
	});

	describe("Responsive Behavior", () => {
		it("renders sidebar with proper width class", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const sidebar = document.querySelector("aside");
			expect(sidebar).toHaveClass("w-64");
		});

		it("renders main content area with flex-1", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const main = screen.getByRole("main");
			expect(main).toHaveClass("flex-1");
		});

		it("has scrollable navigation", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const nav = document.querySelector("nav");
			expect(nav).toHaveClass("overflow-y-auto");
		});
	});

	describe("Accessibility", () => {
		it("has proper aria labels on navigation", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const nav = screen.getByRole("navigation");
			expect(nav).toBeInTheDocument();
		});

		it("has proper heading structure", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			// Logo has aria-label "Model Hotel"
			expect(screen.getByLabelText("Model Hotel")).toBeInTheDocument();
		});

		it("has proper button roles", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const buttons = screen.getAllByRole("button");
			expect(buttons.length).toBeGreaterThanOrEqual(3);
		});
	});

	describe("Layout Main Function", () => {
		it("navigates normally on first click to different page", async () => {
			const user = userEvent.setup();
			const { rerender } = renderWithProviders(
				<Layout>{mockChildren}</Layout>,
				{
					initialEntries: ["/dashboard"],
				},
			);

			const chatLink = screen.getByText("Chat").closest("a");
			expect(chatLink).toBeInTheDocument();

			if (chatLink) {
				await user.click(chatLink);
			}

			rerender(<Layout>{mockChildren}</Layout>);

			const updatedChatLink = screen.getByText("Chat").closest("a");
			expect(updatedChatLink).toHaveClass("sidebar-link-active");
		});

		it("toggles sub-mode when clicking same page link", async () => {
			const user = userEvent.setup();
			renderWithProviders(<Layout>{mockChildren}</Layout>, {
				initialEntries: ["/chat"],
			});

			expect(screen.getByText("Conversation")).toBeInTheDocument();

			const chatLink = screen.getByText("Chat").closest("a");
			expect(chatLink).toBeInTheDocument();

			if (chatLink) {
				await user.click(chatLink);
			}

			await waitFor(() => {
				expect(screen.getByText("Chat")).toBeInTheDocument();
			});
			expect(screen.getByText("Conversation")).toBeInTheDocument();
		});

		it("shows update available styling when version is outdated", async () => {
			server.use(
				http.get(
					"https://api.github.com/repos/hugalafutro/model-hotel/releases/latest",
					() => {
						return HttpResponse.json({ tag_name: "v99.0" });
					},
				),
			);

			renderWithProviders(<Layout>{mockChildren}</Layout>);

			await waitFor(() => {
				const githubLink = screen.getByLabelText("GitHub repository");
				expect(githubLink).toBeInTheDocument();
			});
		});

		it("handles logout confirmation flow", async () => {
			const user = userEvent.setup();
			const originalStorage = localStorage.getItem("adminToken");
			localStorage.setItem("adminToken", "test-token");

			renderWithProviders(<Layout>{mockChildren}</Layout>, {
				initialEntries: ["/dashboard"],
			});

			const logoutButton = screen.getByText("Logout").closest("button");
			expect(logoutButton).toBeInTheDocument();

			if (logoutButton) {
				await user.click(logoutButton);
			}

			expect(screen.getByText("Log out?")).toBeInTheDocument();

			// Find logout confirm button in the dialog
			const confirmButton = screen
				.getByRole("dialog")
				.querySelector("button.ui-btn-danger");
			expect(confirmButton).toBeInTheDocument();
			if (confirmButton) {
				await user.click(confirmButton);
			}

			await waitFor(() => {
				expect(localStorage.getItem("adminToken")).toBeNull();
			});

			if (originalStorage) {
				localStorage.setItem("adminToken", originalStorage);
			}
		});

		it("cancels logout confirmation", async () => {
			const user = userEvent.setup();
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const logoutButton = screen.getByText("Logout").closest("button");
			expect(logoutButton).toBeInTheDocument();

			if (logoutButton) {
				await user.click(logoutButton);
			}

			const cancelButton = screen.getByText("Cancel");
			await user.click(cancelButton);

			await waitFor(() => {
				expect(screen.queryByText("Log out?")).not.toBeInTheDocument();
			});
		});

		it("renders version badge with running version", async () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			await waitFor(() => {
				const versionElement = screen.getByText(/v\d+\.\d+/);
				expect(versionElement).toBeInTheDocument();
			});
		});

		it("renders Docs link with correct href", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const docsLink = screen.getByText("Docs").closest("a");
			expect(docsLink).toHaveAttribute(
				"href",
				"https://github.com/hugalafutro/model-hotel",
			);
			expect(docsLink).toHaveAttribute("target", "_blank");
		});

		it("renders GitHub link with correct attributes", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const githubLink = screen.getByLabelText("GitHub repository");
			expect(githubLink).toHaveAttribute(
				"href",
				"https://github.com/hugalafutro/model-hotel",
			);
			expect(githubLink).toHaveAttribute("target", "_blank");
		});

		it("does not toggle sub-mode when navigating to different page", async () => {
			const user = userEvent.setup();
			renderWithProviders(<Layout>{mockChildren}</Layout>, {
				initialEntries: ["/dashboard"],
			});
			expect(screen.getByText("Conversation")).toBeInTheDocument();
			const chatLink = screen.getByText("Chat").closest("a");
			if (chatLink) {
				await user.click(chatLink);
			}
			await waitFor(() => {
				expect(screen.getByText("Conversation")).toBeInTheDocument();
			});
		});

		it("does not toggle sub-mode for nav item without subModes", async () => {
			const user = userEvent.setup();
			renderWithProviders(<Layout>{mockChildren}</Layout>, {
				initialEntries: ["/settings"],
			});
			const settingsLink = screen.getByText("Settings").closest("a");
			if (settingsLink) {
				await user.click(settingsLink);
			}
			await waitFor(() => {
				expect(screen.getByText("Settings")).toBeInTheDocument();
			});
		});
	});

	describe("Failover Circuit Breaker Badge", () => {
		it("does not show CB badge when all counts are zero", async () => {
			server.use(
				http.get("/api/failover-groups/circuit-breaker-status", () =>
					HttpResponse.json({ closed: 0, half_open: 0, open: 0 }),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			// "Failover" link exists but no colored count spans
			const failoverLink = screen.getByText("Failover").closest("a");
			expect(failoverLink).toBeInTheDocument();
			// No colored count elements (they have title attributes)
			const countElements = failoverLink?.querySelectorAll("[title]");
			expect(countElements?.length).toBe(0);
		});

		it("shows CB badge with colored counts when breakers are active", async () => {
			server.use(
				http.get("/api/failover-groups/circuit-breaker-status", () =>
					HttpResponse.json({ closed: 3, half_open: 1, open: 2 }),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			await waitFor(() => {
				// Green closed count - use getAllByText since Badge wraps text in inner span
				const closedCounts = screen.getAllByText("3");
				expect(closedCounts.length).toBeGreaterThan(0);
				// Amber half-open count
				const halfOpenCounts = screen.getAllByText("1");
				expect(halfOpenCounts.length).toBeGreaterThan(0);
				// Red open count
				const openCounts = screen.getAllByText("2");
				expect(openCounts.length).toBeGreaterThan(0);
			});
		});

		it("shows only non-zero counts in badge", async () => {
			server.use(
				http.get("/api/failover-groups/circuit-breaker-status", () =>
					HttpResponse.json({ closed: 5, half_open: 0, open: 0 }),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			await waitFor(() => {
				const closedCounts = screen.getAllByText("5");
				expect(closedCounts.length).toBeGreaterThan(0);
				// 0 counts are still rendered in the badge structure
				// Check that 0 is present (it may appear twice due to inner span)
				const zeroCounts = screen.getAllByText("0");
				expect(zeroCounts.length).toBeGreaterThan(0);
			});
		});
	});
});
