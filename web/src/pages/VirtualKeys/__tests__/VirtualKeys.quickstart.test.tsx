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
		it("renders cURL and PowerShell snippet windows", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Create a Key")).toBeInTheDocument();
			});

			// cURL and PowerShell are now separate TerminalPreview windows
			expect(
				screen.getByRole("button", { name: /Copy cURL snippet/i }),
			).toBeInTheDocument();
			expect(
				screen.getByRole("button", { name: /Copy PowerShell snippet/i }),
			).toBeInTheDocument();
		});

		it("renders quick start guide when keys exist", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Create a Key")).toBeInTheDocument();
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

			expect(screen.queryByText("Create a Key")).not.toBeInTheDocument();
		});

		it("renders cURL and PowerShell snippet titles", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Create a Key")).toBeInTheDocument();
			});

			// cURL and PowerShell each have their own TerminalPreview title
			const curlTitles = screen.getAllByText("cURL");
			expect(curlTitles.length).toBeGreaterThan(0);
			const psTitles = screen.getAllByText("PowerShell");
			expect(psTitles.length).toBeGreaterThan(0);
		});

		it("shows curl example in cURL snippet window", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Create a Key")).toBeInTheDocument();
			});

			expect(screen.getAllByText(/curl/).length).toBeGreaterThan(0);
			expect(
				screen.getAllByText((content) =>
					content.includes("/v1/chat/completions"),
				).length,
			).toBeGreaterThan(0);
			expect(screen.getAllByText("YOUR_API_KEY").length).toBeGreaterThan(0);
		});

		it("shows PowerShell example in its snippet window", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Create a Key")).toBeInTheDocument();
			});

			// PowerShell content is now always visible (no tab switching needed)
			expect(screen.getByText(/Invoke-RestMethod/)).toBeInTheDocument();
		});

		it("renders CopyButton in cURL snippet window", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Create a Key")).toBeInTheDocument();
			});

			// CopyButton has title attribute with snippet type
			expect(
				screen.getByRole("button", { name: /Copy cURL snippet/i }),
			).toBeInTheDocument();
		});

		it("renders CopyButton for both cURL and PowerShell snippets", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Create a Key")).toBeInTheDocument();
			});

			// Both CopyButtons are always visible (no tab switching)
			expect(
				screen.getByRole("button", { name: /Copy cURL snippet/i }),
			).toBeInTheDocument();
			expect(
				screen.getByRole("button", { name: /Copy PowerShell snippet/i }),
			).toBeInTheDocument();
		});

		it("cURL and PowerShell snippets are both visible simultaneously", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Create a Key")).toBeInTheDocument();
			});

			// Both snippet windows are visible without any tab switching
			expect(
				screen.getByRole("button", { name: /Copy cURL snippet/i }),
			).toBeInTheDocument();
			expect(
				screen.getByRole("button", { name: /Copy PowerShell snippet/i }),
			).toBeInTheDocument();
		});

		it("renders JavaScript example card", async () => {
			server.use(
				http.get("/api/virtual-keys", () =>
					HttpResponse.json([mockVirtualKey]),
				),
			);

			renderWithProviders(<VirtualKeys />);

			await waitFor(() => {
				expect(screen.getByText("Create a Key")).toBeInTheDocument();
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
				expect(screen.getByText("Create a Key")).toBeInTheDocument();
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
				expect(screen.getByText("Create a Key")).toBeInTheDocument();
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
				expect(screen.getByText("Create a Key")).toBeInTheDocument();
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
				expect(screen.getByText("Create a Key")).toBeInTheDocument();
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
				expect(screen.getByText("Create a Key")).toBeInTheDocument();
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
				expect(screen.getByText("Create a Key")).toBeInTheDocument();
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
				expect(screen.getByText("Create a Key")).toBeInTheDocument();
			});

			expect(
				screen.getByRole("button", { name: "Copy OpenCode snippet" }),
			).toBeInTheDocument();
			const opencodeTitles = screen.getAllByText("OpenCode");
			expect(opencodeTitles.length).toBeGreaterThan(0);
		});
	});
});
