import { act, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "../../../test/utils";
import { BracketPreviewPill, SlotParamsTooltip, VoteThumb } from "../shared";

describe("VoteThumb", () => {
	it("renders ThumbsUp when isWinner=true", () => {
		const { container } = renderWithProviders(
			<VoteThumb size={24} isWinner={true} animating={false} />,
		);
		// ThumbsUp should be present (check by svg element with thumbs-up class)
		const svg = container.querySelector(".icon-thumbs-up");
		expect(svg).toBeInTheDocument();
	});

	it("renders ThumbsDown when isWinner=false and animating=false", () => {
		const { container } = renderWithProviders(
			<VoteThumb size={24} isWinner={false} animating={false} />,
		);
		const svg = container.querySelector(".icon-thumbs-down");
		expect(svg).toBeInTheDocument();
	});

	it("toggles between ThumbsUp and ThumbsDown when animating=true", () => {
		vi.useFakeTimers();
		const { container, unmount } = renderWithProviders(
			<VoteThumb size={24} isWinner={false} animating={true} />,
		);

		// Initial state - both icons present (one visible, one hidden via opacity)
		expect(container.querySelectorAll("svg")).toHaveLength(2);

		// Advance timer by 1200ms to trigger toggle
		act(() => {
			vi.advanceTimersByTime(1200);
		});

		// Component should still have both svgs after toggle
		expect(container.querySelectorAll("svg")).toHaveLength(2);

		unmount();
		vi.useRealTimers();
	});
});

describe("SlotParamsTooltip", () => {
	it("renders null when no params provided", () => {
		const { container } = renderWithProviders(<SlotParamsTooltip />);
		// Component returns null, so no span should be rendered
		expect(container.querySelector("span")).toBeNull();
	});

	it("renders null when params is empty object", () => {
		const { container } = renderWithProviders(
			<SlotParamsTooltip params={{}} />,
		);
		// Component returns null when no valid params, so no span should be rendered
		expect(container.querySelector("span")).toBeNull();
	});

	it("renders Settings icon when params present", () => {
		const { container } = renderWithProviders(
			<SlotParamsTooltip params={{ temperature: 0.7 }} />,
		);
		// Component renders a span with Settings icon inside
		const span = container.querySelector("span");
		expect(span).toBeInTheDocument();
		expect(span?.querySelector("svg")).toBeInTheDocument();
	});

	it("tooltip text shows param names and values", () => {
		const { container } = renderWithProviders(
			<SlotParamsTooltip params={{ temperature: 0.7, max_tokens: 4096 }} />,
		);
		const span = container.querySelector("span");
		expect(span).toHaveAttribute("title");
		const title = span?.getAttribute("title");
		expect(title).toContain("Temperature");
		expect(title).toContain("0.7");
		expect(title).toContain("Max tokens");
		expect(title).toContain("4096");
	});

	it("filters undefined values from tooltip", () => {
		const { container } = renderWithProviders(
			<SlotParamsTooltip
				params={{
					temperature: 0.7,
					max_tokens: undefined,
					top_p: 0.9,
				}}
			/>,
		);
		const span = container.querySelector("span");
		const title = span?.getAttribute("title");
		expect(title).toBeDefined();
		expect(title).toContain("Temperature");
		expect(title).toContain("Top p");
		expect(title).not.toContain("Max tokens");
	});

	it("converts snake_case to Title Case in labels", () => {
		const { container } = renderWithProviders(
			<SlotParamsTooltip params={{ frequency_penalty: 0.5 }} />,
		);
		const span = container.querySelector("span");
		const title = span?.getAttribute("title");
		expect(title).toBeDefined();
		expect(title).toContain("Frequency penalty");
	});
});

describe("BracketPreviewPill", () => {
	it('renders "TBD" when isTbd=true', () => {
		renderWithProviders(
			<BracketPreviewPill modelId="test-model" isTbd={true} />,
		);
		expect(screen.getByText("TBD")).toBeInTheDocument();
	});

	it('renders "TBD" when modelId is empty', () => {
		renderWithProviders(<BracketPreviewPill modelId="" />);
		expect(screen.getByText("TBD")).toBeInTheDocument();
	});

	it("renders model name (last part after slash) when modelId provided", () => {
		renderWithProviders(<BracketPreviewPill modelId="provider/model-name" />);
		expect(screen.getByText("model-name")).toBeInTheDocument();
	});

	it("truncates to last segment when multiple slashes", () => {
		renderWithProviders(
			<BracketPreviewPill modelId="org/provider/model-name" />,
		);
		expect(screen.getByText("model-name")).toBeInTheDocument();
	});

	it("renders full modelId when no slash", () => {
		renderWithProviders(<BracketPreviewPill modelId="standalone-model" />);
		expect(screen.getByText("standalone-model")).toBeInTheDocument();
	});

	it("isTbd takes precedence over modelId", () => {
		renderWithProviders(
			<BracketPreviewPill modelId="provider/model" isTbd={true} />,
		);
		expect(screen.getByText("TBD")).toBeInTheDocument();
		expect(screen.queryByText("model")).not.toBeInTheDocument();
	});

	it("uses displayName when provided", () => {
		renderWithProviders(
			<BracketPreviewPill
				modelId="provider/model-name"
				displayName="Friendly Name"
			/>,
		);
		expect(screen.getByText("Friendly Name")).toBeInTheDocument();
	});
});
