import { fireEvent, screen, waitFor } from "@testing-library/react";
import { renderWithProviders } from "../../test/utils";
import { CopyablePill } from "../CopyablePill";

// Stub Lucide icons
vi.mock("@/lib/icons", () => ({
	Briefcase: ({ className }: { className?: string }) => (
		<svg className={className} data-testid="stub-icon" />
	),
}));

describe("CopyablePill", () => {
	beforeEach(() => {
		Object.assign(globalThis, {
			navigator: {
				...globalThis.navigator,
				clipboard: { writeText: vi.fn().mockResolvedValue(undefined) },
			},
		});
	});

	it("renders text", () => {
		renderWithProviders(<CopyablePill text="test@example.com" />);

		expect(screen.getByText("test@example.com")).toBeInTheDocument();
	});

	it("renders displayText when provided", () => {
		renderWithProviders(
			<CopyablePill text="long-text@example.com" displayText="Short" />,
		);

		expect(screen.getByText("Short")).toBeInTheDocument();
		expect(screen.queryByText("long-text@example.com")).not.toBeInTheDocument();
	});

	it("shows toast on successful copy", async () => {
		renderWithProviders(<CopyablePill text="api-key" />);

		fireEvent.click(screen.getByText("api-key"));

		await waitFor(() => {
			expect(screen.getByText("Copied!")).toBeInTheDocument();
		});
	});

	it("renders suffix when provided", () => {
		renderWithProviders(
			<CopyablePill
				text="key"
				suffix={<span data-testid="suffix">Extra</span>}
			/>,
		);

		expect(screen.getByTestId("suffix")).toBeInTheDocument();
		expect(screen.getByText("Extra")).toBeInTheDocument();
	});

	it("shows full text in title even when tooltip prop is provided", () => {
		renderWithProviders(
			<CopyablePill text="my-full-key-value" tooltip="Custom tooltip text" />,
		);

		const button = screen.getByText("my-full-key-value").closest("button");
		// title shows full text (most useful for sighted users on hover)
		expect(button).toHaveAttribute("title", "my-full-key-value");
		// aria-label uses the tooltip prop (short action description for screen readers)
		expect(button).toHaveAttribute("aria-label", "Custom tooltip text");
	});

	it("uses text as default tooltip and aria-label when not provided", () => {
		renderWithProviders(<CopyablePill text="key" />);

		const button = screen.getByText("key").closest("button");
		expect(button).toHaveAttribute("title", "key");
		expect(button).toHaveAttribute("aria-label", "Copy");
	});

	it("applies line-clamp styles when lines > 1", () => {
		renderWithProviders(
			<CopyablePill text="a-very-long-model-id-value" lines={2} />,
		);

		const span = screen.getByText("a-very-long-model-id-value");
		// line-clamp style applied
		expect(span.style.display).toBe("-webkit-box");
		expect(span.style.webkitLineClamp).toBe("2");
		// button uses items-start (icon aligns with first text line)
		const button = span.closest("button") as HTMLButtonElement;
		expect(button.className).toContain("items-start");
		expect(button.className).toContain("text-left");
		// pill does not stretch full width (sizes to content)
		expect(button.className).not.toContain("w-full");
		// default truncate class NOT applied
		expect(span.className).not.toContain("truncate");
	});
});
