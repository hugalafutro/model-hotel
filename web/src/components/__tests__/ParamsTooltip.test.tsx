import { describe, expect, it } from "vitest";
import { renderWithProviders } from "../../test/utils";
import { ParamsTooltip } from "../ParamsTooltip";

describe("ParamsTooltip", () => {
	it("renders null when no params provided", () => {
		const { container } = renderWithProviders(<ParamsTooltip />);
		// Component returns null, so no span should be rendered
		expect(container.querySelector("span")).toBeNull();
	});

	it("renders null when params is empty object", () => {
		const { container } = renderWithProviders(<ParamsTooltip params={{}} />);
		// Component returns null when no valid params, so no span should be rendered
		expect(container.querySelector("span")).toBeNull();
	});

	it("renders Settings icon when params present", () => {
		const { container } = renderWithProviders(
			<ParamsTooltip params={{ temperature: 0.7 }} />,
		);
		// Component renders a span with Settings icon inside
		const span = container.querySelector("span");
		expect(span).toBeInTheDocument();
		expect(span?.querySelector("svg")).toBeInTheDocument();
	});

	it("tooltip text shows param names and values", () => {
		const { container } = renderWithProviders(
			<ParamsTooltip params={{ temperature: 0.7, max_tokens: 4096 }} />,
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
			<ParamsTooltip
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
			<ParamsTooltip params={{ frequency_penalty: 0.5 }} />,
		);
		const span = container.querySelector("span");
		const title = span?.getAttribute("title");
		expect(title).toBeDefined();
		expect(title).toContain("Frequency penalty");
	});

	it("scales the icon to the requested size", () => {
		const { container } = renderWithProviders(
			<ParamsTooltip params={{ temperature: 0.7 }} size={12} />,
		);
		expect(container.querySelector("svg")).toHaveAttribute("width", "12");
	});
});
