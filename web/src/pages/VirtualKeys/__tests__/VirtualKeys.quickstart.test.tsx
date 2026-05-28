import { screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { mockVirtualKey } from "../../../test/mocks/data";
import { server } from "../../../test/mocks/server";
import { renderWithProviders } from "../../../test/utils";
import { VirtualKeys } from "../../VirtualKeys";

describe("VirtualKeys", () => {
	beforeEach(() => {
		server.resetHandlers();
		vi.clearAllMocks();
	});

	describe("Quick Start Section", () => {
		it("renders quick start guide with bash terminal by default", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Quick Start")).toBeInTheDocument();
			});

			// Tab buttons contain SVG + text; use getAllByRole and filter by class
			const bashTabs = screen.getAllByRole("button", { name: /bash/i });
			// First match is the tab (has terminal-tab class), not CopyButton
			const bashTab = bashTabs.find((btn) =>
				btn.classList.contains("terminal-tab"),
			);
			expect(bashTab).toHaveClass("terminal-tab-active");
			const powershellTabs = screen.getAllByRole("button", {
				name: /PowerShell/i,
			});
			const powershellTab = powershellTabs.find((btn) =>
				btn.classList.contains("terminal-tab"),
			);
			expect(powershellTab).toHaveClass("terminal-tab-inactive");
		});

		it("renders quick start guide when keys exist", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Quick Start")).toBeInTheDocument();
			});

			expect(screen.getByText("Create a Key")).toBeInTheDocument();
			expect(screen.getByText("Copy the Full Key")).toBeInTheDocument();
			expect(screen.getByText("Make Requests")).toBeInTheDocument();
		});

		it("does not render quick start when no keys", async () => {
			server.use(http.get("/api/virtual-keys", () => HttpResponse.json([])));

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(
					screen.getByText(
						"No virtual keys. Create one to start using the proxy.",
					),
				).toBeInTheDocument();
			});

			expect(screen.queryByText("Quick Start")).not.toBeInTheDocument();
		});

		it("renders quick start section with collapsible toggle", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Quick Start")).toBeInTheDocument();
			});

			// Verify quick start content is visible
			expect(screen.getByText("Create a Key")).toBeInTheDocument();
			expect(screen.getByText("Copy the Full Key")).toBeInTheDocument();
			expect(screen.getByText("Make Requests")).toBeInTheDocument();

			// Toggle button should exist
			const toggleButton = screen.getByRole("button", {
				name: /collapse|expand|toggle/i,
			});
			expect(toggleButton).toBeInTheDocument();
		});

		it("renders bash and PowerShell tab buttons", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Quick Start")).toBeInTheDocument();
			});

			// Tab bar has buttons with "bash" and "PowerShell" text
			const allButtons = screen.getAllByRole("button");
			const bashButton = allButtons.find((btn) =>
				btn.textContent?.includes("bash"),
			);
			const psButton = allButtons.find((btn) =>
				btn.textContent?.includes("PowerShell"),
			);

			expect(bashButton).toBeInTheDocument();
			expect(psButton).toBeInTheDocument();
		});

		it("shows curl example in bash tab", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Quick Start")).toBeInTheDocument();
			});

			// curl example is in a code block - check for key parts
			expect(screen.getByText(/curl/)).toBeInTheDocument();
			// URL is in a span element
			expect(
				screen.getByText((content) => content.includes("/v1/chat/completions")),
			).toBeInTheDocument();
			expect(screen.getByText("YOUR_API_KEY")).toBeInTheDocument();
		});

		it("shows PowerShell example in powershell tab", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Quick Start")).toBeInTheDocument();
			});

			// Click PowerShell tab
			await user.click(screen.getByRole("button", { name: /powershell/i }));

			// Verify PowerShell content is displayed
			expect(screen.getByText(/Invoke-RestMethod/)).toBeInTheDocument();
		});

		it("renders CopyButton in terminal tab bar", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Quick Start")).toBeInTheDocument();
			});

			// CopyButton has title attribute with snippet type
			expect(
				screen.getByRole("button", { name: /Copy bash snippet/i }),
			).toBeInTheDocument();
		});

		it("CopyButton updates when switching to PowerShell tab", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Quick Start")).toBeInTheDocument();
			});

			const powershellTab = screen.getByRole("button", {
				name: /PowerShell/i,
			});
			await user.click(powershellTab);

			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: /Copy PowerShell snippet/i }),
				).toBeInTheDocument();
			});
		});

		it("switches to PowerShell tab when clicked (line 587)", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Quick Start")).toBeInTheDocument();
			});

			const powershellTabs = screen.getAllByRole("button", {
				name: /PowerShell/i,
			});
			const powershellTab = powershellTabs.find((btn) =>
				btn.classList.contains("terminal-tab"),
			);
			if (!powershellTab) throw new Error("PowerShell tab not found");
			await user.click(powershellTab);

			await waitFor(() => {
				expect(powershellTab).toHaveClass("terminal-tab-active");
			});
			const bashTabs = screen.getAllByRole("button", { name: /bash/i });
			const bashTab = bashTabs.find((btn) =>
				btn.classList.contains("terminal-tab"),
			);
			expect(bashTab).toHaveClass("terminal-tab-inactive");
		});

		it("collapses quick start section", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			const { user } = renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Quick Start")).toBeInTheDocument();
			});

			// CollapsibleToggle defaults: aria-label="Collapse" when expanded
			const collapseToggle = screen.getByRole("button", {
				name: "Collapse",
			});
			await user.click(collapseToggle);

			// After clicking, button label changes to "Expand"
			await waitFor(() => {
				expect(
					screen.getByRole("button", { name: "Expand" }),
				).toBeInTheDocument();
			});
		});

		it("renders JavaScript example card", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Quick Start")).toBeInTheDocument();
			});

			// JavaScript title appears in card header; verify CopyButton exists
			expect(
				screen.getByRole("button", { name: "Copy JavaScript snippet" }),
			).toBeInTheDocument();
			// Check title exists (may appear multiple times due to code content)
			const jsTitles = screen.getAllByText("JavaScript");
			expect(jsTitles.length).toBeGreaterThan(0);
		});

		it("renders Python example card", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Quick Start")).toBeInTheDocument();
			});

			// Python title appears in card header; verify CopyButton exists
			expect(
				screen.getByRole("button", { name: "Copy Python snippet" }),
			).toBeInTheDocument();
			// Check title exists (may appear multiple times due to code content)
			const pythonTitles = screen.getAllByText("Python");
			expect(pythonTitles.length).toBeGreaterThan(0);
		});

		it("renders Claude Code example card", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Quick Start")).toBeInTheDocument();
			});

			// Claude Code title appears in card header; verify CopyButton exists
			expect(
				screen.getByRole("button", { name: "Copy Claude Code snippet" }),
			).toBeInTheDocument();
			// Check title exists (may appear multiple times due to code content)
			const claudeCodeTitles = screen.getAllByText("Claude Code");
			expect(claudeCodeTitles.length).toBeGreaterThan(0);
		});

		it("renders OpenClaw example card", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Quick Start")).toBeInTheDocument();
			});

			// OpenClaw title appears in card header; verify CopyButton exists
			expect(
				screen.getByRole("button", { name: "Copy OpenClaw snippet" }),
			).toBeInTheDocument();
			// Check title exists (may appear multiple times due to code content)
			const openclawTitles = screen.getAllByText("OpenClaw");
			expect(openclawTitles.length).toBeGreaterThan(0);
		});

		it("renders Hermes example card", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Quick Start")).toBeInTheDocument();
			});

			// Hermes title appears in card header; code content has lowercase "hermes"
			// Verify CopyButton exists
			expect(
				screen.getByRole("button", { name: "Copy Hermes snippet" }),
			).toBeInTheDocument();
			// Check title exists (may appear multiple times due to code content)
			const hermesTitles = screen.getAllByText("Hermes");
			expect(hermesTitles.length).toBeGreaterThan(0);
		});

		it("renders LibreChat example card", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Quick Start")).toBeInTheDocument();
			});

			expect(
				screen.getByRole("button", { name: "Copy LibreChat snippet" }),
			).toBeInTheDocument();
			const libreChatTitles = screen.getAllByText("LibreChat");
			expect(libreChatTitles.length).toBeGreaterThan(0);
		});

		it("renders ZED example card", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Quick Start")).toBeInTheDocument();
			});

			expect(
				screen.getByRole("button", { name: "Copy ZED snippet" }),
			).toBeInTheDocument();
			const zedTitles = screen.getAllByText("ZED");
			expect(zedTitles.length).toBeGreaterThan(0);
		});

		it("renders OpenCode example card", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Quick Start")).toBeInTheDocument();
			});

			expect(
				screen.getByRole("button", { name: "Copy OpenCode snippet" }),
			).toBeInTheDocument();
			const opencodeTitles = screen.getAllByText("OpenCode");
			expect(opencodeTitles.length).toBeGreaterThan(0);
		});
	});
});
