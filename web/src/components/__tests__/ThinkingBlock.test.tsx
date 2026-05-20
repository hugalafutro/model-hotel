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

	it("uses inline-flex for toggle button (only text+chevron is clickable area)", () => {
		renderWithProviders(
			<ThinkingBlock thinking={thinkingText} isStreaming={false} />,
		);

		const button = screen.getByText("Thinking").closest("button");
		expect(button).toHaveClass("inline-flex");
		expect(button).not.toHaveClass("w-full");
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

	it("scrolls to bottom when unrolled during streaming", async () => {
		// jsdom doesn't compute layout, so we mock scroll dimensions.
		// We intercept scrollTop assignments to verify the scroll-to-bottom behavior.
		const longThinking = Array(200).fill("Line of thinking text").join("\n");

		// Track scrollTop assignments on the scrollable container
		const scrollTopCalls: number[] = [];
		const origDescriptor = Object.getOwnPropertyDescriptor(
			HTMLElement.prototype,
			"scrollTop",
		);

		Object.defineProperty(HTMLElement.prototype, "scrollTop", {
			get(this: HTMLDivElement) {
				// Return a mock value if the element has the overflow-y-auto class
				if (this.className?.includes("overflow-y-auto")) {
					return scrollTopCalls.length > 0
						? scrollTopCalls[scrollTopCalls.length - 1]
						: 0;
				}
				return origDescriptor?.get?.call(this) ?? 0;
			},
			set(this: HTMLDivElement, v: number) {
				if (this.className?.includes("overflow-y-auto")) {
					scrollTopCalls.push(v);
					return;
				}
				origDescriptor?.set?.call(this, v);
			},
			configurable: true,
		});

		// Also mock scrollHeight/clientHeight for the nearBottom check
		const origSH = Object.getOwnPropertyDescriptor(
			HTMLElement.prototype,
			"scrollHeight",
		);
		Object.defineProperty(HTMLElement.prototype, "scrollHeight", {
			get(this: HTMLDivElement) {
				if (this.className?.includes("overflow-y-auto")) return 600;
				return origSH?.get?.call(this) ?? 0;
			},
			configurable: true,
		});
		const origCH = Object.getOwnPropertyDescriptor(
			HTMLElement.prototype,
			"clientHeight",
		);
		Object.defineProperty(HTMLElement.prototype, "clientHeight", {
			get(this: HTMLDivElement) {
				if (this.className?.includes("overflow-y-auto")) return 240;
				return origCH?.get?.call(this) ?? 0;
			},
			configurable: true,
		});

		try {
			renderWithProviders(
				<ThinkingBlock thinking={longThinking} isStreaming={true} />,
			);

			// Unroll — should trigger scroll to bottom via rAF
			fireEvent.click(screen.getByText("Thinking"));

			await vi.waitFor(() => {
				// The scroll-on-unroll effect should have set scrollTop = scrollHeight (600)
				expect(scrollTopCalls.some((v) => v === 600)).toBe(true);
			});
		} finally {
			// Restore original descriptors
			if (origDescriptor)
				Object.defineProperty(
					HTMLElement.prototype,
					"scrollTop",
					origDescriptor,
				);
			if (origSH)
				Object.defineProperty(HTMLElement.prototype, "scrollHeight", origSH);
			if (origCH)
				Object.defineProperty(HTMLElement.prototype, "clientHeight", origCH);
		}
	});
});
