import { fireEvent, render, screen } from "@testing-library/react";
import { FilterInput } from "../FilterInput";

// Stub Lucide icons
vi.mock("@/lib/icons", () => ({
	X: ({ className }: { className?: string }) => (
		<svg className={className} data-testid="x-icon" />
	),
}));

describe("FilterInput", () => {
	it("renders input with placeholder", () => {
		render(
			<FilterInput value="" onChange={vi.fn()} placeholder="Search items" />,
		);

		const input = screen.getByPlaceholderText("Search items");
		expect(input).toBeInTheDocument();
		expect(input).toHaveValue("");
	});

	it("calls onChange when typing", () => {
		const onChange = vi.fn();
		render(<FilterInput value="" onChange={onChange} />);

		const input = screen.getByPlaceholderText("Filter…");
		fireEvent.change(input, { target: { value: "test" } });

		expect(onChange).toHaveBeenCalledWith("test");
	});

	it("shows clear button when value is present", () => {
		render(<FilterInput value="existing value" onChange={vi.fn()} />);

		expect(
			screen.getByRole("button", { name: "Clear filter" }),
		).toBeInTheDocument();
	});

	it("hides clear button when value is empty", () => {
		render(<FilterInput value="" onChange={vi.fn()} />);

		expect(
			screen.queryByRole("button", { name: "Clear filter" }),
		).not.toBeInTheDocument();
	});

	it("clears value when clicking X button", () => {
		const onChange = vi.fn();
		render(<FilterInput value="test" onChange={onChange} />);

		fireEvent.click(screen.getByRole("button", { name: "Clear filter" }));

		expect(onChange).toHaveBeenCalledWith("");
	});

	it("respects disabled prop", () => {
		const onChange = vi.fn();
		render(
			<FilterInput
				value=""
				onChange={onChange}
				placeholder="Disabled input"
				disabled
			/>,
		);

		const input = screen.getByPlaceholderText("Disabled input");
		expect(input).toBeDisabled();
	});

	it("respects autoFocus prop", () => {
		render(
			<FilterInput
				value=""
				onChange={vi.fn()}
				placeholder="Auto-focused"
				autoFocus
			/>,
		);

		const input = screen.getByPlaceholderText("Auto-focused");
		expect(input).toHaveFocus();
	});

	it("uses default placeholder when not provided", () => {
		render(<FilterInput value="" onChange={vi.fn()} />);

		expect(screen.getByPlaceholderText("Filter…")).toBeInTheDocument();
	});

	it("respects className prop", () => {
		render(
			<FilterInput value="" onChange={vi.fn()} className="custom-class" />,
		);

		const container = screen.getByPlaceholderText("Filter…").parentElement;
		expect(container).toHaveClass("custom-class");
	});

	it("respects id prop", () => {
		render(<FilterInput value="" onChange={vi.fn()} id="filter-input" />);

		const input = screen.getByPlaceholderText("Filter…");
		expect(input).toHaveAttribute("id", "filter-input");
	});
});
