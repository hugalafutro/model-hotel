import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderWithProviders } from "../../test/utils";
import { TerminalPreview } from "../TerminalPreview";

describe("TerminalPreview", () => {
	const defaultProps = {
		variant: "bash" as const,
		copyText: "echo hello",
		children: <span data-testid="test-content">Test Content</span>,
	};

	describe("Bash variant", () => {
		it("renders macOS-style titlebar with 3 colored dots", () => {
			const { container } = renderWithProviders(
				<TerminalPreview {...defaultProps} />,
			);

			// Verify the 3 colored dots exist (red, yellow, green)
			const dots = container.querySelectorAll(
				".rounded-full.bg-red-500, .rounded-full.bg-yellow-500, .rounded-full.bg-green-500",
			);
			expect(dots.length).toBe(3);
		});

		it("renders default 'bash' title", () => {
			renderWithProviders(<TerminalPreview {...defaultProps} />);

			expect(screen.getByText("bash")).toBeInTheDocument();
		});

		it("renders children content", () => {
			renderWithProviders(<TerminalPreview {...defaultProps} />);

			expect(screen.getByTestId("test-content")).toBeInTheDocument();
			expect(screen.getByText("Test Content")).toBeInTheDocument();
		});

		it("renders CopyButton with correct copyText", () => {
			renderWithProviders(<TerminalPreview {...defaultProps} />);

			// CopyButton should have aria-label from title prop
			const copyButton = screen.getByRole("button", {
				name: "Copy bash snippet",
			});
			expect(copyButton).toBeInTheDocument();
		});

		it("applies fixed height when height prop is set", () => {
			const { container } = renderWithProviders(
				<TerminalPreview {...defaultProps} height={200} />,
			);

			// Outer container should have explicit height style instead of min-h-70
			const outerDiv = container.querySelector(".relative");
			expect(outerDiv).toBeInTheDocument();
			expect(outerDiv?.style.height).toBe("200px");
			expect(outerDiv?.className).not.toContain("min-h-70");

			// Pre should have overflow-y-auto for scrollable content
			const pre = container.querySelector("pre");
			expect(pre?.className).toContain("overflow-y-auto");
		});

		it("uses min-h-70 when height prop is not set", () => {
			const { container } = renderWithProviders(
				<TerminalPreview {...defaultProps} />,
			);

			const outerDiv = container.querySelector(".relative");
			expect(outerDiv).toBeInTheDocument();
			expect(outerDiv?.style.height).toBe("");
			expect(outerDiv?.className).toContain("min-h-70");
		});
	});

	describe("PowerShell variant", () => {
		it("renders Windows 11 titlebar with Windows icon SVG", () => {
			const { container } = renderWithProviders(
				<TerminalPreview {...defaultProps} variant="powershell" />,
			);

			// Windows icon SVG should be present
			const svgIcons = container.querySelectorAll("svg.win11-icon");
			expect(svgIcons.length).toBe(1);
		});

		it("renders default 'PowerShell' title", () => {
			renderWithProviders(
				<TerminalPreview {...defaultProps} variant="powershell" />,
			);

			expect(screen.getByText("PowerShell")).toBeInTheDocument();
		});

		it("renders children content", () => {
			renderWithProviders(
				<TerminalPreview {...defaultProps} variant="powershell" />,
			);

			expect(screen.getByTestId("test-content")).toBeInTheDocument();
			expect(screen.getByText("Test Content")).toBeInTheDocument();
		});

		it("renders CopyButton with correct copyText", () => {
			renderWithProviders(
				<TerminalPreview {...defaultProps} variant="powershell" />,
			);

			const copyButton = screen.getByRole("button", {
				name: "Copy PowerShell snippet",
			});
			expect(copyButton).toBeInTheDocument();
		});

		it("applies fixed height when height prop is set", () => {
			const { container } = renderWithProviders(
				<TerminalPreview {...defaultProps} variant="powershell" height={200} />,
			);

			// Outer container should have explicit height style instead of min-h-70
			const outerDiv = container.querySelector(".terminal-win11");
			expect(outerDiv).toBeInTheDocument();
			expect(outerDiv?.style.height).toBe("200px");
			expect(outerDiv?.className).not.toContain("min-h-70");

			// Pre should have overflow-y-auto for scrollable content
			const pre = container.querySelector("pre");
			expect(pre?.className).toContain("overflow-y-auto");
		});

		it("uses min-h-70 when height prop is not set", () => {
			const { container } = renderWithProviders(
				<TerminalPreview {...defaultProps} variant="powershell" />,
			);

			const outerDiv = container.querySelector(".terminal-win11");
			expect(outerDiv).toBeInTheDocument();
			expect(outerDiv?.style.height).toBe("");
			expect(outerDiv?.className).toContain("min-h-70");
		});
	});

	describe("Custom title", () => {
		it("overrides default title with custom title", () => {
			renderWithProviders(
				<TerminalPreview {...defaultProps} title="Streaming" />,
			);

			expect(screen.getByText("Streaming")).toBeInTheDocument();
			expect(screen.queryByText("bash")).not.toBeInTheDocument();
		});

		it("overrides PowerShell default title with custom title", () => {
			renderWithProviders(
				<TerminalPreview
					{...defaultProps}
					variant="powershell"
					title="Custom Terminal"
				/>,
			);

			expect(screen.getByText("Custom Terminal")).toBeInTheDocument();
			expect(screen.queryByText("PowerShell")).not.toBeInTheDocument();
		});
	});

	describe("CopyButton positioning", () => {
		it("is positioned inside the terminal titlebar", () => {
			const { container } = renderWithProviders(
				<TerminalPreview {...defaultProps} />,
			);

			// Find the terminal container (first div with relative positioning)
			const terminalContainer = container.querySelector(".relative");
			expect(terminalContainer).toBeInTheDocument();

			// CopyButton should be inside the terminal container
			const copyButton = screen.getByRole("button", {
				name: "Copy bash snippet",
			});
			expect(terminalContainer?.contains(copyButton)).toBe(true);

			const buttonWrapper = copyButton.parentElement;
			expect(buttonWrapper?.className).toContain("ml-auto");
		});

		it("is positioned inside PowerShell terminal container", () => {
			const { container } = renderWithProviders(
				<TerminalPreview {...defaultProps} variant="powershell" />,
			);

			// Find the terminal container
			const terminalContainer = container.querySelector(".terminal-win11");
			expect(terminalContainer).toBeInTheDocument();

			// CopyButton should be inside
			const copyButton = screen.getByRole("button", {
				name: "Copy PowerShell snippet",
			});
			expect(terminalContainer?.contains(copyButton)).toBe(true);
		});
	});

	describe("Code variant", () => {
		it("renders code block with title", () => {
			renderWithProviders(
				<TerminalPreview
					variant="code"
					title="JavaScript"
					copyText="import OpenAI from 'openai'"
				>
					<span>test content</span>
				</TerminalPreview>,
			);

			expect(screen.getByText("JavaScript")).toBeInTheDocument();
		});

		it("renders icon when provided", () => {
			const { container } = renderWithProviders(
				<TerminalPreview
					variant="code"
					title="JavaScript"
					icon="javascript"
					copyText="import OpenAI from 'openai'"
				>
					<span>test content</span>
				</TerminalPreview>,
			);

			// SVG with title "JavaScript" should be present in the header
			const svgIcons = container.querySelectorAll("svg");
			const javascriptIcon = Array.from(svgIcons).find(
				(svg) => svg.querySelector("title")?.textContent === "JavaScript",
			);
			expect(javascriptIcon).toBeInTheDocument();
		});

		it("renders without icon", () => {
			const { container } = renderWithProviders(
				<TerminalPreview
					variant="code"
					title="Python"
					copyText="print('hello')"
				>
					<span>test content</span>
				</TerminalPreview>,
			);

			// Title text should render
			expect(screen.getByText("Python")).toBeInTheDocument();

			// No LangIcon SVG in the header (only the CopyButton SVG)
			const headerDiv = container.querySelector(".bg-gray-900\\/50");
			const svgIcons = headerDiv?.querySelectorAll("svg");
			// CopyButton has an SVG, but no LangIcon SVGs
			expect(svgIcons?.length).toBe(1);
			// Lucide Copy icon has no <title> element
			expect(svgIcons?.[0]?.querySelector("title")).toBeNull();
		});

		it("renders CopyButton with correct aria-label", () => {
			renderWithProviders(
				<TerminalPreview
					variant="code"
					title="JavaScript"
					icon="javascript"
					copyText="import OpenAI from 'openai'"
				>
					<span>test content</span>
				</TerminalPreview>,
			);

			const copyButton = screen.getByRole("button", {
				name: "Copy JavaScript snippet",
			});
			expect(copyButton).toBeInTheDocument();
		});

		it("renders children content", () => {
			renderWithProviders(
				<TerminalPreview
					variant="code"
					title="JavaScript"
					copyText="import OpenAI from 'openai'"
				>
					<span>test content</span>
				</TerminalPreview>,
			);

			expect(screen.getByText("test content")).toBeInTheDocument();
		});

		it("renders rounded-lg container", () => {
			const { container } = renderWithProviders(
				<TerminalPreview
					variant="code"
					title="JavaScript"
					copyText="import OpenAI from 'openai'"
				>
					<span>test content</span>
				</TerminalPreview>,
			);

			// The outer div should have rounded-lg (unlike bash which has rounded-b-lg rounded-tr-lg)
			const outerDiv = container.querySelector(".rounded-lg");
			expect(outerDiv).toBeInTheDocument();
		});

		it("renders librechat icon", () => {
			const { container } = renderWithProviders(
				<TerminalPreview
					variant="code"
					title="LibreChat"
					icon="librechat"
					copyText="endpoints:"
				>
					<span>test content</span>
				</TerminalPreview>,
			);

			const imgIcon = container.querySelector('img[alt="LibreChat"]');
			expect(imgIcon).toBeInTheDocument();
		});

		it("applies fixed height when height prop is set", () => {
			const { container } = renderWithProviders(
				<TerminalPreview
					variant="code"
					title="cURL"
					copyText="curl example"
					height={200}
				>
					<span>test content</span>
				</TerminalPreview>,
			);

			const pre = container.querySelector("pre");
			expect(pre).toBeInTheDocument();
			expect(pre?.style.height).toBe("200px");
			expect(pre?.className).toContain("overflow-y-auto");
		});

		it("reserves bottom padding for horizontal scrollbar", () => {
			const { container } = renderWithProviders(
				<TerminalPreview
					variant="code"
					title="cURL"
					copyText="curl example"
					height={200}
				>
					<span>test content</span>
				</TerminalPreview>,
			);

			const pre = container.querySelector("pre");
			expect(pre?.className).toContain("pb-6");
		});
	});
});
