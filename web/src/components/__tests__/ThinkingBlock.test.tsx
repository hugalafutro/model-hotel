import { fireEvent, screen } from "@testing-library/react";
import { renderWithProviders } from "../../test/utils";
import { ThinkingBlock } from "../ThinkingBlock";

// Stub Lucide icons
vi.mock("lucide-react", () => ({
	Brain: ({ className }: { className?: string }) => (
		<svg className={className} data-testid="brain-icon" />
	),
	ChevronDown: ({ className }: { className?: string }) => (
		<svg className={className} data-testid="chevron-down-icon" />
	),
	ChevronRight: ({ className }: { className?: string }) => (
		<svg className={className} data-testid="chevron-right-icon" />
	),
}));

describe("ThinkingBlock", () => {
	const thinkingText = "This is some thinking content";

	it("renders collapsed by default", () => {
		renderWithProviders(
			<ThinkingBlock thinking={thinkingText} isStreaming={false} />,
		);

		expect(screen.getByText("Thinking")).toBeInTheDocument();
		expect(screen.getByTestId("chevron-right-icon")).toBeInTheDocument();
		expect(screen.queryByText(thinkingText)).not.toBeInTheDocument();
	});

	it("expands to show thinking text when clicked", () => {
		renderWithProviders(
			<ThinkingBlock thinking={thinkingText} isStreaming={false} />,
		);

		fireEvent.click(screen.getByText("Thinking"));

		expect(screen.getByText(thinkingText)).toBeInTheDocument();
		expect(screen.getByTestId("chevron-down-icon")).toBeInTheDocument();
	});

	it("shows streaming indicator when isStreaming is true", () => {
		renderWithProviders(
			<ThinkingBlock thinking={thinkingText} isStreaming={true} />,
		);

		const button = screen.getByText("Thinking").closest("button");
		expect(button).toHaveClass("animate-pulse");
	});

	it("collapses when clicked again after expanding", () => {
		renderWithProviders(
			<ThinkingBlock thinking={thinkingText} isStreaming={false} />,
		);

		// Expand
		fireEvent.click(screen.getByText("Thinking"));
		expect(screen.getByText(thinkingText)).toBeInTheDocument();

		// Collapse
		fireEvent.click(screen.getByText("Thinking"));
		expect(screen.queryByText(thinkingText)).not.toBeInTheDocument();
		expect(screen.getByTestId("chevron-right-icon")).toBeInTheDocument();
	});

	it("strips leading newlines from thinking text", () => {
		const thinkingWithNewlines = "\n\n\nActual content here";
		renderWithProviders(
			<ThinkingBlock thinking={thinkingWithNewlines} isStreaming={false} />,
		);

		fireEvent.click(screen.getByText("Thinking"));
		expect(screen.getByText("Actual content here")).toBeInTheDocument();
	});
});
