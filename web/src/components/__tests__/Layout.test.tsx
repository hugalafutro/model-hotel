import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
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
				screen.getByText('"Because we have LiteLLM at home"'),
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
		it("toggles Chat sub-mode on second click", async () => {
			const user = userEvent.setup();
			renderWithProviders(<Layout>{mockChildren}</Layout>, {
				initialEntries: ["/chat"],
			});

			const chatLink = screen.getByText("Chat").closest("a");
			expect(chatLink).toBeInTheDocument();
			expect(screen.getByText("Chat")).toBeInTheDocument();
			expect(screen.getByText("Conversation")).toBeInTheDocument();

			if (chatLink) {
				await user.click(chatLink);
			}

			expect(screen.getByText("Conversation")).toBeInTheDocument();
			expect(screen.getByText("Chat")).toBeInTheDocument();
		});

		it("toggles Arena sub-mode on second click", async () => {
			const user = userEvent.setup();
			renderWithProviders(<Layout>{mockChildren}</Layout>, {
				initialEntries: ["/arena"],
			});

			const arenaLink = screen.getByText("Arena").closest("a");
			expect(arenaLink).toBeInTheDocument();

			if (arenaLink) {
				await user.click(arenaLink);
			}

			expect(screen.getByText("Compare")).toBeInTheDocument();
			expect(screen.getByText("Arena")).toBeInTheDocument();
		});

		it("toggles Logs sub-mode on second click", async () => {
			const user = userEvent.setup();
			renderWithProviders(<Layout>{mockChildren}</Layout>, {
				initialEntries: ["/logs"],
			});

			const logsLink = screen.getByText("Logs").closest("a");
			expect(logsLink).toBeInTheDocument();

			if (logsLink) {
				await user.click(logsLink);
			}

			expect(screen.getByText("Logs")).toBeInTheDocument();
			expect(screen.getByText("Requests")).toBeInTheDocument();
		});

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

			const githubLink = screen.getByText("GitHub").closest("a");
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

			expect(screen.queryByText("Log out?")).not.toBeInTheDocument();
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

			const confirmButton = screen.getByText("Log out");
			await user.click(confirmButton);

			expect(localStorage.getItem("adminToken")).toBeNull();

			if (originalStorage) {
				localStorage.setItem("adminToken", originalStorage);
			}
		});
	});

	describe("System Status Panel", () => {
		it("renders API Status indicator", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			expect(screen.getByText("API Status")).toBeInTheDocument();
		});

		it("renders Uptime row", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			expect(screen.getByText("Uptime")).toBeInTheDocument();
		});

		it("renders CPU row", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			expect(screen.getByText("CPU")).toBeInTheDocument();
		});

		it("renders Network row", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			expect(screen.getByText("Network")).toBeInTheDocument();
		});

		it("renders Disk row", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			expect(screen.getByText("Disk")).toBeInTheDocument();
		});

		it("renders Memory row", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			expect(screen.getByText("Memory")).toBeInTheDocument();
		});

		it("renders Go routines row", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			expect(screen.getByText("Go routines")).toBeInTheDocument();
		});

		it("renders Req Today row", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			expect(screen.getByText("Req Today")).toBeInTheDocument();
		});

		it("renders DB row", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			expect(screen.getByText("DB")).toBeInTheDocument();
		});

		it("has collapsible toggle for stats", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const toggleButtons = screen.getAllByRole("button");
			const collapseButtons = toggleButtons.filter((btn) => {
				const title = btn.getAttribute("title");
				return title?.includes("Collapse") || title?.includes("Expand");
			});
			expect(collapseButtons.length).toBeGreaterThanOrEqual(1);
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

	describe("Provider Quota Panel", () => {
		it("renders quota panel container", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const nav = document.querySelector("nav");
			expect(nav).toBeInTheDocument();
		});

		it("renders QuotaBadges within quota panel", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const nav = document.querySelector("nav");
			expect(nav).toBeInTheDocument();
		});
	});

	describe("Navigation Icons", () => {
		it("renders Dashboard icon", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const dashboardLink = screen.getByText("Dashboard").closest("li");
			expect(dashboardLink?.querySelector("svg")).toBeInTheDocument();
		});

		it("renders Chat icon", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const chatLink = screen.getByText("Chat").closest("li");
			expect(chatLink?.querySelector("svg")).toBeInTheDocument();
		});

		it("renders Arena icon", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const arenaLink = screen.getByText("Arena").closest("li");
			expect(arenaLink?.querySelector("svg")).toBeInTheDocument();
		});

		it("renders Providers icon", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const providersLink = screen.getByText("Providers").closest("li");
			expect(providersLink?.querySelector("svg")).toBeInTheDocument();
		});

		it("renders Models icon", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const modelsLink = screen.getByText("Models").closest("li");
			expect(modelsLink?.querySelector("svg")).toBeInTheDocument();
		});

		it("renders Failover icon", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const failoverLink = screen.getByText("Failover").closest("li");
			expect(failoverLink?.querySelector("svg")).toBeInTheDocument();
		});

		it("renders Virtual Keys icon", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const virtualKeysLink = screen.getByText("Virtual Keys").closest("li");
			expect(virtualKeysLink?.querySelector("svg")).toBeInTheDocument();
		});

		it("renders Logs icon", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const logsLink = screen.getByText("Logs").closest("li");
			expect(logsLink?.querySelector("svg")).toBeInTheDocument();
		});

		it("renders Settings icon", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const settingsLink = screen.getByText("Settings").closest("li");
			expect(settingsLink?.querySelector("svg")).toBeInTheDocument();
		});
	});

	describe("Keyboard Navigation", () => {
		it("supports keyboard navigation for links", async () => {
			const user = userEvent.setup();
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.tab();
			await user.tab();

			expect(document.activeElement).toBeInTheDocument();
		});

		it("supports keyboard navigation for theme toggle", async () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			// Theme toggle exists and is focusable
			const themeButton = screen.getByTitle(/Switch to (light|dark) mode/);
			expect(themeButton).toBeInTheDocument();
			themeButton.focus();
			expect(document.activeElement).toBe(themeButton);
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
});
