import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { server } from "../../test/mocks/server";
import { renderWithProviders } from "../../test/utils";
import * as webauthnUtils from "../../utils/webauthn";
import { Layout } from "../Layout";

describe("Layout", () => {
	const mockChildren = <div data-testid="main-content">Page Content</div>;

	beforeEach(() => {
		vi.clearAllMocks();
		// Auth is the readable mh_csrf cookie (seeded once in setup.ts). The logout
		// tests clear it, so re-seed before every test to keep the suite
		// order-independent.
		document.cookie = "mh_csrf=test-csrf; path=/";
	});

	describe("Sidebar Navigation", () => {
		it("renders sidebar with logo", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			// Logo has aria-label "Model Hotel"
			expect(screen.getByLabelText("Model Hotel")).toBeInTheDocument();
			// The 'Multi-Provider AI Gateway' subtitle lives on the login
			// screen only; the sidebar shows just the logo and tagline.
			expect(
				screen.queryByText("Multi-Provider AI Gateway"),
			).not.toBeInTheDocument();
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
		it("renders Wiki link", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const docsLink = screen.getByText("Wiki").closest("a");
			expect(docsLink).toHaveAttribute(
				"href",
				"https://github.com/hugalafutro/model-hotel/wiki",
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

		it("renders the logout button with its accessible Logout name", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			// The button keeps its accessible "Logout" name (aria-label) even though
			// its visible label is the logged-in user's name. Identity rendering is
			// locked separately in Layout.identity.test.tsx (this harness has no
			// IdentityProvider, so `me` is null and the label falls back to "Logout").
			expect(
				screen.getByRole("button", { name: "Logout" }),
			).toBeInTheDocument();
		});

		it("opens logout confirmation dialog on logout click", async () => {
			const user = userEvent.setup();
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const logoutButton = screen.getByRole("button", { name: "Logout" });
			expect(logoutButton).toBeInTheDocument();

			await user.click(logoutButton);

			expect(screen.getByText("Log out?")).toBeInTheDocument();
			expect(
				screen.getByText("You'll need to re-enter your admin token."),
			).toBeInTheDocument();
		});

		it("closes logout confirmation on Cancel click", async () => {
			const user = userEvent.setup();
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const logoutButton = screen.getByRole("button", { name: "Logout" });
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
			document.cookie = "mh_csrf=test-csrf; path=/";

			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const logoutButton = screen.getByRole("button", { name: "Logout" });
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
				expect(document.cookie).not.toContain("mh_csrf=");
			});
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
			document.cookie = "mh_csrf=test-csrf; path=/";

			renderWithProviders(<Layout>{mockChildren}</Layout>, {
				initialEntries: ["/dashboard"],
			});

			const logoutButton = screen.getByRole("button", { name: "Logout" });
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
				expect(document.cookie).not.toContain("mh_csrf=");
			});
		});

		it("cancels logout confirmation", async () => {
			const user = userEvent.setup();
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const logoutButton = screen.getByRole("button", { name: "Logout" });
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

		it("renders Wiki link with correct href", () => {
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const docsLink = screen.getByText("Wiki").closest("a");
			expect(docsLink).toHaveAttribute(
				"href",
				"https://github.com/hugalafutro/model-hotel/wiki",
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

		it("shows CB badge with half-open/open counts when breakers are active", async () => {
			server.use(
				http.get("/api/failover-groups/circuit-breaker-status", () =>
					HttpResponse.json({ closed: 3, half_open: 1, open: 2 }),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			await waitFor(() => {
				// Amber half-open count
				const halfOpenCounts = screen.getAllByText("1");
				expect(halfOpenCounts.length).toBeGreaterThan(0);
				// Red open count
				const openCounts = screen.getAllByText("2");
				expect(openCounts.length).toBeGreaterThan(0);
			});

			// The healthy (closed) count is no longer surfaced in the badge —
			// it was almost always just the provider count and confused users.
			const failoverLink = screen.getByText("Failover").closest("a");
			expect(failoverLink?.textContent).not.toContain("3");
		});

		it("does not show the badge when only healthy (closed) providers exist", async () => {
			server.use(
				http.get("/api/failover-groups/circuit-breaker-status", () =>
					HttpResponse.json({ closed: 5, half_open: 0, open: 0 }),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			// The badge only appears when something is recovering or tripped, so a
			// fully-healthy fleet shows no badge and never surfaces the count "5".
			const failoverLink = screen.getByText("Failover").closest("a");
			await waitFor(() => {
				expect(failoverLink?.querySelectorAll("[title]").length).toBe(0);
			});
			expect(failoverLink?.textContent).not.toContain("5");
		});

		it("shows tooltip with open/half-open provider names", async () => {
			server.use(
				http.get("/api/failover-groups/circuit-breaker-status", () =>
					HttpResponse.json({
						closed: 1,
						half_open: 1,
						open: 2,
						providers: [
							{
								provider_id: "p-1",
								provider_name: "Healthy Provider",
								state: "closed",
								consecutive_fails: 0,
							},
							{
								provider_id: "p-2",
								provider_name: "Wobbly Provider",
								state: "half-open",
								consecutive_fails: 2,
							},
							{
								provider_id: "p-3",
								provider_name: "Down Provider",
								state: "open",
								consecutive_fails: 5,
							},
							{
								provider_id: "p-4",
								provider_name: "Also Down",
								state: "open",
								consecutive_fails: 3,
							},
						],
					}),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			await waitFor(() => {
				// The badge pill should have a title listing only unhealthy providers
				const badge = screen
					.getByText("Failover")
					.closest("a")
					?.querySelector("[title]");
				expect(badge).toBeInTheDocument();
				const tooltip = badge?.getAttribute("title");
				expect(tooltip).toContain("Wobbly Provider");
				expect(tooltip).toContain("Down Provider");
				expect(tooltip).toContain("Also Down");
				expect(tooltip).not.toContain("Healthy Provider");
			});
		});

		/** The badge tooltip, split into its lines. */
		function badgeTooltipLines(): string[] {
			const tooltip = screen
				.getByText("Failover")
				.closest("a")
				?.querySelector("[title]")
				?.getAttribute("title");
			return tooltip?.split("\n") ?? [];
		}

		async function renderWithProviderStatuses(
			providers: Record<string, unknown>[],
		) {
			server.use(
				http.get("/api/failover-groups/circuit-breaker-status", () =>
					HttpResponse.json({
						closed: 0,
						half_open: providers.filter((p) => p.state === "half-open").length,
						open: providers.filter((p) => p.state === "open").length,
						providers,
					}),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);
			await waitFor(() => {
				expect(badgeTooltipLines().length).toBeGreaterThan(1);
			});
		}

		it("lists quota-pinned providers on their own tooltip line", async () => {
			await renderWithProviderStatuses([
				{
					provider_id: "p-1",
					provider_name: "Down Provider",
					state: "open",
					consecutive_fails: 5,
				},
				{
					provider_id: "p-2",
					provider_name: "Quota Provider",
					state: "open",
					consecutive_fails: 5,
					quota_pinned: true,
				},
			]);

			// Explanation line, ordinary-cooldown line, quota-pinned line. The two
			// provider lines must be distinct so a week-long pin is not read as a
			// sixty-second cooldown.
			const lines = badgeTooltipLines();
			expect(lines).toHaveLength(3);
			const pinnedLine = lines.find((l) => l.includes("Quota Provider"));
			const ordinaryLine = lines.find((l) => l.includes("Down Provider"));
			expect(pinnedLine).toBeDefined();
			expect(ordinaryLine).toBeDefined();
			expect(pinnedLine).not.toContain("Down Provider");
			expect(ordinaryLine).not.toContain("Quota Provider");
		});

		it("moves a pinned provider off the quota line once its pin has expired", async () => {
			// The backend keeps quota_pinned set for the whole life of the pinned
			// circuit and re-reports it as half-open once the deadline passes, with
			// no next_retry_at. Reading quota_pinned alone would keep claiming the
			// provider is waiting on a quota window that has already reset.
			await renderWithProviderStatuses([
				{
					provider_id: "p-1",
					provider_name: "Still Pinned",
					state: "open",
					consecutive_fails: 5,
					quota_pinned: true,
				},
				{
					provider_id: "p-2",
					provider_name: "Pin Expired",
					state: "half-open",
					consecutive_fails: 5,
					quota_pinned: true,
				},
			]);

			const lines = badgeTooltipLines();
			expect(lines).toHaveLength(3);
			const pinnedLine = lines.find((l) => l.includes("Still Pinned"));
			const ordinaryLine = lines.find((l) => l.includes("Pin Expired"));
			expect(pinnedLine).toBeDefined();
			expect(ordinaryLine).toBeDefined();
			// The expired pin belongs with the ordinary ready-to-probe providers,
			// not with the ones still waiting on a reset.
			expect(pinnedLine).not.toContain("Pin Expired");
			expect(ordinaryLine).not.toContain("Still Pinned");
		});

		it("omits the ordinary-cooldown line when every unhealthy provider is pinned", async () => {
			await renderWithProviderStatuses([
				{
					provider_id: "p-1",
					provider_name: "Quota One",
					state: "open",
					consecutive_fails: 5,
					quota_pinned: true,
				},
				{
					provider_id: "p-2",
					provider_name: "Quota Two",
					state: "open",
					consecutive_fails: 5,
					quota_pinned: true,
				},
			]);

			// Explanation line plus the quota line only: an empty ordinary bucket
			// must not render a line claiming zero providers.
			const lines = badgeTooltipLines();
			expect(lines).toHaveLength(2);
			expect(lines[1]).toContain("Quota One");
			expect(lines[1]).toContain("Quota Two");
		});
	});

	describe("Discovery Discrepancies Badge", () => {
		const claim = (model_id: string) => ({
			model_id,
			state: "gone",
			last_seen_at: "2026-07-01T00:00:00Z",
			missing_scans: 3,
			flap_window: 0,
			flap_since_review: 0,
		});

		const providerClaims = (
			provider_id: string,
			provider_name: string,
			gone: ReturnType<typeof claim>[],
		) => ({ provider_id, provider_name, gone, stale: [], suspect: [] });

		const status = (over: Record<string, unknown> = {}) => ({
			claims: [],
			group_claims: [],
			informational: [],
			claim_count: 0,
			informational_unseen: 0,
			...over,
		});

		const infoEntry = {
			provider_id: "p1",
			provider_name: "One",
			source: "background",
			detected_at: "2026-07-25T00:00:00Z",
			diff: { added: [{ model_id: "brand-new", reason: "new model" }] },
		};

		it("shows the claim count on the Models badge", async () => {
			server.use(
				http.get("/api/discovery/status", () =>
					HttpResponse.json(status({ claim_count: 3 })),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const badge = await screen.findByTestId("discovery-status-badge");
			expect(badge).toHaveAttribute("data-variant", "count");
			expect(badge).toHaveTextContent("3");
			expect(badge.getAttribute("aria-label")).not.toMatch(/^layout\.nav\./);
		});

		it("shows a dot rather than a number when only informational news is unseen", async () => {
			// The badge means "things that might be wrong". A price move is news, not
			// a problem, so it gets attention once without ever showing a count.
			server.use(
				http.get("/api/discovery/status", () =>
					HttpResponse.json(status({ informational_unseen: 4 })),
				),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const badge = await screen.findByTestId("discovery-status-badge");
			expect(badge).toHaveAttribute("data-variant", "dot");
			expect(badge.textContent).toBe("");
			// With no text, the accessible name is the control's ONLY affordance, so
			// an unresolved key here is not cosmetic. Matched on the key prefix
			// rather than on the copy, to stay locale-independent.
			expect(badge.getAttribute("aria-label")).not.toMatch(/^layout\.nav\./);
		});

		it("hides the badge when there is nothing at all", async () => {
			let fetches = 0;
			server.use(
				http.get("/api/discovery/status", () => {
					fetches++;
					return HttpResponse.json(status());
				}),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			// Wait for a real answer first, so this cannot pass merely because the
			// query had not resolved yet.
			await waitFor(() => expect(fetches).toBeGreaterThanOrEqual(1));
			await screen.findByRole("navigation");
			expect(screen.queryByTestId("discovery-status-badge")).toBeNull();
		});

		it("never stamps the review marker from the badge poll", async () => {
			// ?review=1 rebaselines "since your last visit" server-side. A poll doing
			// that would hold every flap count at zero forever.
			const urls: string[] = [];
			server.use(
				http.get("/api/discovery/status", ({ request }) => {
					urls.push(request.url);
					return HttpResponse.json(status({ claim_count: 1 }));
				}),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			await screen.findByTestId("discovery-status-badge");
			expect(urls.length).toBeGreaterThan(0);
			expect(urls.some((u) => u.includes("review=1"))).toBe(false);
		});

		it("renders the failover group claims that the badge count includes", async () => {
			// claim_count counts discovery-disabled failover groups. If the modal
			// cannot show them the badge points at rows that do not exist, which is
			// the badge-that-lies defect this rework exists to remove.
			server.use(
				http.get("/api/discovery/status", () =>
					HttpResponse.json(
						status({
							claim_count: 1,
							group_claims: [
								{
									display_model: "gpt-oss-120b",
									member_count: 3,
									routable_count: 1,
									disabled_at: "2026-07-20T00:00:00Z",
								},
							],
						}),
					),
				),
			);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));

			const rows = await screen.findAllByTestId("discrepancy-group-claim");
			expect(rows).toHaveLength(1);
			expect(rows[0]).toHaveAttribute("data-display-model", "gpt-oss-120b");
			expect(screen.queryByTestId("discrepancy-empty")).toBeNull();
		});

		it("refetches and re-stamps the review marker on a second open", async () => {
			// The hook keys its fetch on a counter that only advances on an open
			// transition, which works only while the hook stays mounted. If Layout
			// ever unmounts it on close the counter resets, the first key is reused,
			// the cache answers, and both bugs return: stale rows on reopen and a
			// review stamp that stops firing per open.
			let reviewStamps = 0;
			server.use(
				http.get("/api/discovery/status", ({ request }) => {
					const review =
						new URL(request.url).searchParams.get("review") === "1";
					if (review) reviewStamps++;
					return HttpResponse.json(
						status({
							claim_count: 1,
							claims: [
								providerClaims("p1", "NanoGPT", [
									claim(reviewStamps <= 1 ? "first-open" : "second-open"),
								]),
							],
						}),
					);
				}),
			);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			expect(await screen.findByTestId("discrepancy-claim")).toHaveAttribute(
				"data-model-id",
				"first-open",
			);
			await waitFor(() => expect(reviewStamps).toBe(1));

			// Escape, not the close button: that control is labelled with a
			// translated string and this suite stays locale-independent.
			fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
			await waitFor(() =>
				expect(screen.queryByTestId("discrepancy-modal")).toBeNull(),
			);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			// A cache replay would put the first open's row back and never reach the
			// server, so this pins both halves at once: a real refetch, and a real
			// second stamp.
			await waitFor(() =>
				expect(screen.getByTestId("discrepancy-claim")).toHaveAttribute(
					"data-model-id",
					"second-open",
				),
			);
			expect(reviewStamps).toBe(2);
		});

		it("stops Retest all after the provider already in flight", async () => {
			const discovered: string[] = [];
			let release: (() => void) | undefined;
			server.use(
				http.get("/api/discovery/status", () =>
					HttpResponse.json(
						status({
							claim_count: 2,
							claims: [
								providerClaims("p1", "One", [claim("a")]),
								providerClaims("p2", "Two", [claim("b")]),
							],
						}),
					),
				),
				http.post("/api/providers/:id/discover", async ({ params }) => {
					discovered.push(String(params.id));
					await new Promise<void>((resolve) => {
						release = resolve;
					});
					return HttpResponse.json({ discovered: 0, diff: {} });
				}),
			);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			await user.click(await screen.findByTestId("discrepancy-retest-all"));

			// p1's discovery run is out; Cancel has taken the Retest all slot.
			await waitFor(() => expect(discovered).toEqual(["p1"]));
			expect(screen.queryByTestId("discrepancy-retest-all")).toBeNull();
			await user.click(
				await screen.findByTestId("discrepancy-retest-all-cancel"),
			);

			// Cancel must not abort p1: a half-applied discovery run is worse than a
			// slow one, so the walk only declines to start p2.
			release?.();
			await waitFor(() =>
				expect(screen.queryByTestId("discrepancy-retest-progress")).toBeNull(),
			);
			expect(discovered).toEqual(["p1"]);
			// The walk stopped early, so it must not sign off as a completed run.
			// Asserted on the toast's type rather than its text, to stay
			// locale-independent: a success toast here is the "done: 1" bug.
			expect(await screen.findByTestId("toast")).toHaveAttribute(
				"data-toast-type",
				"info",
			);
		});

		it("reports a Retest all walk once, not once per provider", async () => {
			server.use(
				http.get("/api/discovery/status", () =>
					HttpResponse.json(
						status({
							claim_count: 3,
							claims: [
								providerClaims("p1", "One", [claim("a")]),
								providerClaims("p2", "Two", [claim("b")]),
								providerClaims("p3", "Three", [claim("c")]),
							],
						}),
					),
				),
			);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			await user.click(await screen.findByTestId("discrepancy-retest-all"));

			await waitFor(() =>
				expect(screen.queryByTestId("discrepancy-retest-progress")).toBeNull(),
			);
			// Three providers, one toast. ToastContext dedupes by message and each
			// per-provider message names a different provider, so nothing would
			// collapse them if the walk did not silence them.
			expect(screen.getAllByTestId("toast")).toHaveLength(1);
		});

		it("dismisses one model per request and offers an undo that restores it", async () => {
			const bodies: { model_ids: string[]; dismissed: boolean }[] = [];
			const dismissed = new Set<string>();
			server.use(
				http.get("/api/discovery/status", () => {
					const gone = ["a", "b"]
						.filter((m) => !dismissed.has(m))
						.map((m) => claim(m));
					return HttpResponse.json(
						status({
							claim_count: gone.length,
							claims: [providerClaims("p1", "One", gone)],
						}),
					);
				}),
				http.post("/api/discovery/dismiss", async ({ request }) => {
					const body = (await request.json()) as {
						model_ids: string[];
						dismissed: boolean;
					};
					bodies.push(body);
					for (const m of body.model_ids) {
						if (body.dismissed) dismissed.add(m);
						else dismissed.delete(m);
					}
					return HttpResponse.json({ updated: body.model_ids.length });
				}),
			);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			const rowA = () =>
				screen
					.getAllByTestId("discrepancy-claim")
					.find((el) => el.getAttribute("data-model-id") === "a");
			await waitFor(() => expect(rowA()).toBeTruthy());
			await user.click(
				rowA()?.querySelector(
					'[data-testid="discrepancy-dismiss"]',
				) as HTMLElement,
			);

			// Struck through in place rather than removed: the row that vanishes on a
			// click is the operator complaint this rework exists to fix.
			await waitFor(() =>
				expect(rowA()).toHaveAttribute("data-status", "resolved"),
			);
			// Exactly one model per request. A mixed batch 200s with a short
			// `updated` and cannot say which ids it missed.
			expect(bodies).toHaveLength(1);
			expect(bodies[0].model_ids).toEqual(["a"]);
			expect(bodies[0].dismissed).toBe(true);

			await user.click(await screen.findByTestId("toast-action"));
			await waitFor(() => expect(bodies).toHaveLength(2));
			expect(bodies[1].model_ids).toEqual(["a"]);
			expect(bodies[1].dismissed).toBe(false);
			await waitFor(() =>
				expect(rowA()).toHaveAttribute("data-status", "pending"),
			);
		});

		it("treats updated: 0 as a failed dismissal", async () => {
			// The endpoint 200s with a short `updated` and only 404s when nothing at
			// all matched, so HTTP status alone would report a phantom success.
			server.use(
				http.get("/api/discovery/status", () =>
					HttpResponse.json(
						status({
							claim_count: 1,
							claims: [providerClaims("p1", "One", [claim("a")])],
						}),
					),
				),
				http.post("/api/discovery/dismiss", () =>
					HttpResponse.json({ updated: 0 }),
				),
			);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			await user.click(await screen.findByTestId("discrepancy-dismiss"));

			// No confirmation toast, so no Undo control: the success path is the only
			// path that renders one.
			await waitFor(() =>
				expect(screen.getByTestId("discrepancy-claim")).toHaveAttribute(
					"data-status",
					"pending",
				),
			);
			expect(screen.queryByTestId("toast-action")).toBeNull();
		});

		it("acknowledges the journal once it is expanded, without re-stamping review", async () => {
			let acks = 0;
			let reviewStamps = 0;
			server.use(
				http.get("/api/discovery/status", ({ request }) => {
					if (new URL(request.url).searchParams.get("review") === "1") {
						reviewStamps++;
					}
					return HttpResponse.json(
						status({
							claim_count: 1,
							claims: [providerClaims("p1", "One", [claim("a")])],
							informational: [infoEntry],
							informational_unseen: 1,
						}),
					);
				}),
				http.post("/api/discovery/changes/ack", () => {
					acks++;
					return HttpResponse.json({ entries: [], count: 0 });
				}),
			);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			const toggle = await screen.findByTestId(
				"discrepancy-informational-toggle",
			);
			// Opening must NOT ack. The destructive ack-on-open is what let the badge
			// clear while the problem was still outstanding.
			expect(acks).toBe(0);
			await waitFor(() => expect(reviewStamps).toBe(1));

			await user.click(toggle);
			await waitFor(() => expect(acks).toBe(1));

			// The ack's follow-up invalidation must be `exact`. Query keys match by
			// prefix, so a non-exact invalidate of ["discovery-status"] also refetches
			// the modal's ["discovery-status","modal",n] query, which fetches with
			// review=1 and moves the server's "since your last visit" baseline to now
			// — silently zeroing every flap count for the next visit, on a routine
			// click rather than a timer.
			await waitFor(() => expect(acks).toBe(1));
			expect(reviewStamps).toBe(1);
		});

		it("does not re-stamp review when an SSE event lands while the modal is open", async () => {
			// Same prefix-matching hazard as the ack path, on the listener that fires
			// exactly when flap counts have just moved.
			let reviewStamps = 0;
			let polls = 0;
			server.use(
				http.get("/api/discovery/status", ({ request }) => {
					if (new URL(request.url).searchParams.get("review") === "1") {
						reviewStamps++;
					} else {
						polls++;
					}
					return HttpResponse.json(
						status({
							claim_count: 1,
							claims: [providerClaims("p1", "One", [claim("a")])],
						}),
					);
				}),
			);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			await screen.findByTestId("discrepancy-claim");
			await waitFor(() => expect(reviewStamps).toBe(1));
			const pollsBefore = polls;

			await act(async () => {
				window.dispatchEvent(
					new CustomEvent("server-event", {
						detail: { type: "discovery.changes_pending" },
					}),
				);
			});

			// The badge poll must refetch (that is the point of the listener) while
			// the modal's review query must not.
			await waitFor(() => expect(polls).toBeGreaterThan(pollsBefore));
			expect(reviewStamps).toBe(1);
		});

		it("shows a failure state instead of the empty state when the fetch fails", async () => {
			server.use(
				http.get("/api/discovery/status", ({ request }) => {
					// The poll succeeds so the badge renders; only the modal's own
					// review fetch fails.
					if (new URL(request.url).searchParams.get("review") === "1") {
						return HttpResponse.json({ error: "boom" }, { status: 500 });
					}
					return HttpResponse.json(status({ claim_count: 2 }));
				}),
			);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));

			expect(
				await screen.findByTestId("discrepancy-load-error"),
			).toBeInTheDocument();
			// "Nothing is wrong" when we could not find out is the same false
			// reassurance this rework exists to remove.
			expect(screen.queryByTestId("discrepancy-empty")).toBeNull();
		});

		it("shows a loading state instead of the empty state while the fetch is out", async () => {
			// One step earlier than the failure case above: the operator clicked a
			// badge reading 1, and until the answer lands the modal knows nothing.
			// Telling them there are no discrepancies in that window is the same
			// false reassurance, just shorter-lived.
			let release: (() => void) | undefined;
			const gate = new Promise<void>((resolve) => {
				release = resolve;
			});
			let modalFetches = 0;
			server.use(
				http.get("/api/discovery/status", async ({ request }) => {
					// Only the modal's review fetch is held; the badge poll answers
					// immediately so the badge is there to click.
					if (new URL(request.url).searchParams.get("review") === "1") {
						modalFetches++;
						await gate;
					}
					return HttpResponse.json(
						status({
							claim_count: 1,
							claims: [providerClaims("p1", "NanoGPT", [claim("a")])],
						}),
					);
				}),
			);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			await screen.findByTestId("discrepancy-modal");
			await waitFor(() => expect(modalFetches).toBe(1));

			expect(screen.queryByTestId("discrepancy-empty")).toBeNull();
			expect(screen.getByTestId("discrepancy-loading")).toBeInTheDocument();

			release?.();
			expect(await screen.findByTestId("discrepancy-claim")).toHaveAttribute(
				"data-model-id",
				"a",
			);
			// And the loading line is a state, not a permanent header.
			expect(screen.queryByTestId("discrepancy-loading")).toBeNull();
		});

		it("does not paint the previous session's rows when reopened", async () => {
			// Close clears what the last visit collected. Without that the second
			// open renders the first open's snapshot until its own fetch lands:
			// struck-through resolved rows and already-dismissed models, presented
			// as the current state of the world.
			let opens = 0;
			let release: (() => void) | undefined;
			server.use(
				http.get("/api/discovery/status", async ({ request }) => {
					if (new URL(request.url).searchParams.get("review") === "1") {
						opens++;
						if (opens === 2) {
							await new Promise<void>((resolve) => {
								release = resolve;
							});
						}
					}
					return HttpResponse.json(
						status({
							claim_count: 1,
							claims: [
								providerClaims("p1", "NanoGPT", [
									claim(opens <= 1 ? "first-open" : "second-open"),
								]),
							],
						}),
					);
				}),
			);
			const { user } = renderWithProviders(<Layout>{mockChildren}</Layout>);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			expect(await screen.findByTestId("discrepancy-claim")).toHaveAttribute(
				"data-model-id",
				"first-open",
			);

			// Escape, not the close button: that control is labelled with a
			// translated string and this suite stays locale-independent.
			fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
			await waitFor(() =>
				expect(screen.queryByTestId("discrepancy-modal")).toBeNull(),
			);

			await user.click(await screen.findByTestId("discovery-status-badge"));
			await screen.findByTestId("discrepancy-modal");
			await waitFor(() => expect(opens).toBe(2));

			// The second open's fetch is still out, so there is nothing yet to show.
			expect(screen.queryByTestId("discrepancy-claim")).toBeNull();
			expect(screen.getByTestId("discrepancy-loading")).toBeInTheDocument();

			release?.();
			await waitFor(() =>
				expect(screen.getByTestId("discrepancy-claim")).toHaveAttribute(
					"data-model-id",
					"second-open",
				),
			);
		});

		it("refetches the badge on a discovery.changes_pending SSE event", async () => {
			let fetches = 0;
			server.use(
				http.get("/api/discovery/status", () => {
					fetches++;
					return HttpResponse.json(status());
				}),
			);
			renderWithProviders(<Layout>{mockChildren}</Layout>);

			await waitFor(() => expect(fetches).toBeGreaterThanOrEqual(1));
			const initial = fetches;

			await act(async () => {
				window.dispatchEvent(
					new CustomEvent("server-event", {
						detail: { type: "discovery.changes_pending" },
					}),
				);
			});

			await waitFor(() => expect(fetches).toBeGreaterThan(initial));
		});
	});

	describe("SSE Event Handling", () => {
		it("invalidates circuit breaker query on circuit_breaker SSE event", async () => {
			let fetchCount = 0;
			server.use(
				http.get("/api/failover-groups/circuit-breaker-status", () => {
					fetchCount++;
					return HttpResponse.json({
						closed: 1,
						half_open: 0,
						open: 0,
					});
				}),
			);

			renderWithProviders(<Layout>{mockChildren}</Layout>);

			// Wait for initial fetch
			await waitFor(() => {
				expect(fetchCount).toBeGreaterThanOrEqual(1);
			});

			const initialCount = fetchCount;

			// Fire a circuit_breaker SSE event — should trigger refetch
			const event = new CustomEvent("server-event", {
				detail: { type: "circuit_breaker.open", message: "Provider down" },
			});
			await act(async () => {
				window.dispatchEvent(event);
			});

			await waitFor(() => {
				expect(fetchCount).toBeGreaterThan(initialCount);
			});

			// Non-matching event should not trigger additional refetch
			const countAfterMatch = fetchCount;
			const nonMatchingEvent = new CustomEvent("server-event", {
				detail: { type: "provider.created", message: "New provider" },
			});
			await act(async () => {
				window.dispatchEvent(nonMatchingEvent);
			});

			// Give a tick for any potential refetch
			await new Promise((r) => setTimeout(r, 100));
			expect(fetchCount).toBe(countAfterMatch);
		});
	});

	describe("Logout", () => {
		it("revokes the session via the always-mounted logout endpoint", async () => {
			const user = userEvent.setup();
			vi.spyOn(webauthnUtils, "isWebAuthnAvailable").mockResolvedValue(true);
			let logoutCalled = false;
			server.use(
				http.post("/api/auth/logout", () => {
					logoutCalled = true;
					return HttpResponse.json({ success: true });
				}),
			);
			document.cookie = "mh_csrf=test-csrf; path=/";

			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const logoutButton = screen.getByRole("button", { name: "Logout" });
			expect(logoutButton).toBeInTheDocument();
			if (logoutButton) {
				await user.click(logoutButton);
			}

			const confirmButton = screen
				.getByRole("dialog")
				.querySelector("button.ui-btn-danger");
			if (confirmButton) {
				await user.click(confirmButton);
			}

			await waitFor(() => {
				expect(logoutCalled).toBe(true);
				expect(document.cookie).not.toContain("mh_csrf=");
			});
		});

		// Regression: logout must clear the client auth signal so queries don't
		// refetch with a just-revoked session in the gap before the reload,
		// producing a burst of 401s server-side.
		it("clears the session cookie on logout", async () => {
			const user = userEvent.setup();
			vi.spyOn(webauthnUtils, "isWebAuthnAvailable").mockResolvedValue(true);
			document.cookie = "mh_csrf=test-csrf; path=/";
			server.use(
				http.post("/api/auth/logout", () =>
					HttpResponse.json({ success: true }),
				),
			);

			renderWithProviders(<Layout>{mockChildren}</Layout>);

			const logoutButton = screen.getByRole("button", { name: "Logout" });
			if (logoutButton) {
				await user.click(logoutButton);
			}
			const confirmButton = screen
				.getByRole("dialog")
				.querySelector("button.ui-btn-danger");
			if (confirmButton) {
				await user.click(confirmButton as HTMLElement);
			}

			await waitFor(() => {
				expect(document.cookie).not.toContain("mh_csrf=");
			});
		});
	});
});
