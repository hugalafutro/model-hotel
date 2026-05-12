import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { FilterDropdown } from "../FilterDropdown";

describe("FilterDropdown", () => {
	const options = [
		{ value: "option1", label: "Option 1" },
		{ value: "option2", label: "Option 2" },
		{ value: "option3", label: "Option 3" },
	];
	const onChange = vi.fn();

	beforeEach(() => {
		onChange.mockClear();
	});

	it("renders trigger button with placeholder", () => {
		render(<FilterDropdown options={options} value="" onChange={onChange} />);
		expect(screen.getByText("Filter")).toBeInTheDocument();
	});

	it("renders trigger button with selected value", () => {
		render(
			<FilterDropdown options={options} value="option1" onChange={onChange} />,
		);
		expect(screen.getByText("Option 1")).toBeInTheDocument();
	});

	it("opens dropdown when trigger is clicked", async () => {
		const user = userEvent.setup();
		render(<FilterDropdown options={options} value="" onChange={onChange} />);
		// Target the trigger button by its label text "Filter"
		await user.click(screen.getByRole("button", { name: "Filter" }));
		expect(screen.getByText("All")).toBeInTheDocument();
		expect(screen.getByText("Option 1")).toBeInTheDocument();
		expect(screen.getByText("Option 2")).toBeInTheDocument();
		expect(screen.getByText("Option 3")).toBeInTheDocument();
	});

	it("calls onChange with empty string when 'All' is selected", async () => {
		const user = userEvent.setup();
		render(
			<FilterDropdown options={options} value="option1" onChange={onChange} />,
		);
		// Target the trigger button by its label text "Option 1"
		await user.click(screen.getByRole("button", { name: "Option 1" }));
		await user.click(screen.getByText("All"));
		expect(onChange).toHaveBeenCalledWith("");
	});

	it("calls onChange with selected option value", async () => {
		const user = userEvent.setup();
		render(<FilterDropdown options={options} value="" onChange={onChange} />);
		// Target the trigger button by its label text "Filter"
		await user.click(screen.getByRole("button", { name: "Filter" }));
		await user.click(screen.getByText("Option 2"));
		expect(onChange).toHaveBeenCalledWith("option2");
	});

	it("closes dropdown after selection", async () => {
		const user = userEvent.setup();
		render(<FilterDropdown options={options} value="" onChange={onChange} />);
		// Target the trigger button by its label text "Filter"
		await user.click(screen.getByRole("button", { name: "Filter" }));
		expect(screen.getByText("All")).toBeInTheDocument();
		await user.click(screen.getByText("Option 1"));
		expect(screen.queryByText("All")).not.toBeInTheDocument();
	});

	it("closes dropdown when clicking outside", async () => {
		const user = userEvent.setup();
		render(<FilterDropdown options={options} value="" onChange={onChange} />);
		// Target the trigger button by its label text "Filter"
		await user.click(screen.getByRole("button", { name: "Filter" }));
		expect(screen.getByText("All")).toBeInTheDocument();
		await user.click(document.body);
		expect(screen.queryByText("All")).not.toBeInTheDocument();
	});

	it("shows clear button when value is selected", () => {
		render(
			<FilterDropdown options={options} value="option1" onChange={onChange} />,
		);
		const clearButton = screen.getByRole("button", { name: "Clear filter" });
		expect(clearButton).toBeInTheDocument();
	});

	it("hides clear button when no value is selected", () => {
		render(<FilterDropdown options={options} value="" onChange={onChange} />);
		expect(
			screen.queryByRole("button", { name: "Clear filter" }),
		).not.toBeInTheDocument();
	});

	it("calls onChange with empty string when clear button is clicked", async () => {
		const user = userEvent.setup();
		render(
			<FilterDropdown options={options} value="option1" onChange={onChange} />,
		);
		const clearButton = screen.getByRole("button", { name: "Clear filter" });
		await user.click(clearButton);
		expect(onChange).toHaveBeenCalledWith("");
	});

	it("displays count when provided in options", async () => {
		const optionsWithCount = [
			{ value: "option1", label: "Option 1", count: 5 },
			{ value: "option2", label: "Option 2", count: 10 },
		];
		const user = userEvent.setup();
		render(
			<FilterDropdown
				options={optionsWithCount}
				value=""
				onChange={onChange}
			/>,
		);
		// Target the trigger button by its label text "Filter"
		await user.click(screen.getByRole("button", { name: "Filter" }));
		expect(screen.getByText("(5)")).toBeInTheDocument();
		expect(screen.getByText("(10)")).toBeInTheDocument();
	});

	it("uses custom placeholder when provided", () => {
		render(
			<FilterDropdown
				options={options}
				value=""
				onChange={onChange}
				placeholder="Select..."
			/>,
		);
		expect(screen.getByText("Select...")).toBeInTheDocument();
	});

	it("uses custom allLabel when provided", async () => {
		const user = userEvent.setup();
		render(
			<FilterDropdown
				options={options}
				value=""
				onChange={onChange}
				allLabel="Show All"
			/>,
		);
		// Target the trigger button by its label text "Filter"
		await user.click(screen.getByRole("button", { name: "Filter" }));
		expect(screen.getByText("Show All")).toBeInTheDocument();
	});

	it("applies custom className", () => {
		render(
			<FilterDropdown
				options={options}
				value=""
				onChange={onChange}
				className="custom-class"
			/>,
		);
		// Target the trigger button by its label text "Filter"
		expect(
			screen.getByRole("button", { name: "Filter" }).parentElement,
		).toHaveClass("custom-class");
	});

	it("highlights selected option in dropdown", async () => {
		const user = userEvent.setup();
		render(
			<FilterDropdown options={options} value="option2" onChange={onChange} />,
		);
		// Target the trigger button by its label text "Option 2"
		await user.click(screen.getByRole("button", { name: "Option 2" }));
		// Find the dropdown option button containing "Option 2" text (not the trigger)
		const selectedOption = screen.getAllByText("Option 2")[1].closest("button");
		expect(selectedOption).toHaveClass("bg-(--accent-light)");
	});

	it("shows chevron icon", () => {
		render(<FilterDropdown options={options} value="" onChange={onChange} />);
		// Target the trigger button by its label text "Filter"
		const triggerButton = screen.getByRole("button", { name: "Filter" });
		// ChevronDown icon should be present
		expect(triggerButton.querySelector("svg")).toBeInTheDocument();
	});
});
