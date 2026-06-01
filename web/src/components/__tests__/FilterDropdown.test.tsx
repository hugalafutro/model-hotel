import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithProviders } from "../../test/utils";
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
		expect(screen.getByText("All")).toBeInTheDocument();
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
		// aria-label is the placeholder "Filter", visible text shows "All"
		await user.click(screen.getByRole("button", { name: "Filter" }));
		// The trigger button also shows "All", so check for option texts which are unique
		expect(screen.getByText("Option 1")).toBeInTheDocument();
		expect(screen.getByText("Option 2")).toBeInTheDocument();
		expect(screen.getByText("Option 3")).toBeInTheDocument();
	});

	it("calls onChange with empty string when 'All' is selected", async () => {
		const user = userEvent.setup();
		render(
			<FilterDropdown options={options} value="option1" onChange={onChange} />,
		);
		// aria-label is "Filter: Option 1" when value is selected, visible text shows selected value
		await user.click(screen.getByRole("button", { name: "Filter: Option 1" }));
		await user.click(screen.getByText("All"));
		expect(onChange).toHaveBeenCalledWith("");
	});

	it("calls onChange with selected option value", async () => {
		const user = userEvent.setup();
		render(<FilterDropdown options={options} value="" onChange={onChange} />);
		// aria-label is the placeholder "Filter", visible text shows "All"
		await user.click(screen.getByRole("button", { name: "Filter" }));
		await user.click(screen.getByText("Option 2"));
		expect(onChange).toHaveBeenCalledWith("option2");
	});

	it("closes dropdown after selection", async () => {
		const user = userEvent.setup();
		// Component is controlled - need to track value state to see change
		renderWithProviders(
			<FilterDropdown options={options} value="option1" onChange={onChange} />,
		);
		// aria-label is "Filter: Option 1" when value is selected
		await user.click(screen.getByRole("button", { name: "Filter: Option 1" }));
		// Verify dropdown is open by checking for option texts
		expect(screen.getByText("Option 2")).toBeInTheDocument();
		// Click outside (on a different option) to close
		await user.click(screen.getByText("Option 2"));
		// After selection, dropdown closes - verify other options are gone
		expect(screen.queryByText("Option 3")).not.toBeInTheDocument();
	});

	it("closes dropdown when clicking outside", async () => {
		const user = userEvent.setup();
		renderWithProviders(
			<FilterDropdown options={options} value="" onChange={onChange} />,
		);
		// Open dropdown - aria-label is "Filter" (default placeholder)
		await user.click(screen.getByRole("button", { name: "Filter" }));
		// Verify options are visible
		expect(screen.getByText("Option 1")).toBeInTheDocument();
		// Click outside to close
		await user.click(document.body);
		// Verify options are hidden after closing
		await waitFor(() => {
			expect(screen.queryByText("Option 1")).not.toBeInTheDocument();
		});
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
		// aria-label is "Filter" (default placeholder)
		await user.click(screen.getByRole("button", { name: "Filter" }));
		expect(screen.getByText("(5)")).toBeInTheDocument();
		expect(screen.getByText("(10)")).toBeInTheDocument();
	});

	it("uses custom allLabel as visible text", () => {
		render(
			<FilterDropdown
				options={options}
				value=""
				onChange={onChange}
				allLabel="Select..."
				placeholder="Filter by..."
			/>,
		);
		// aria-label is the custom placeholder, visible text shows custom allLabel
		expect(
			screen.getByRole("button", { name: "Filter by..." }),
		).toBeInTheDocument();
		expect(screen.getByText("Select...")).toBeInTheDocument();
	});

	it("uses custom allLabel and placeholder", async () => {
		const user = userEvent.setup();
		render(
			<FilterDropdown
				options={options}
				value=""
				onChange={onChange}
				allLabel="Show All"
				placeholder="Pick one"
			/>,
		);
		// aria-label is the custom placeholder, visible text shows custom allLabel
		expect(
			screen.getByRole("button", { name: "Pick one" }),
		).toBeInTheDocument();
		expect(screen.getByText("Show All")).toBeInTheDocument();
		// Clicking opens dropdown with options
		await user.click(screen.getByRole("button", { name: "Pick one" }));
		expect(screen.getByText("Option 1")).toBeInTheDocument();
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
		// aria-label is default placeholder "Filter"
		expect(
			screen.getByRole("button", { name: "Filter" }).parentElement,
		).toHaveClass("custom-class");
	});

	it("highlights selected option in dropdown", async () => {
		const user = userEvent.setup();
		render(
			<FilterDropdown options={options} value="option2" onChange={onChange} />,
		);
		// aria-label is "Filter: Option 2" when value is selected
		await user.click(screen.getByRole("button", { name: "Filter: Option 2" }));
		// Find the dropdown option button containing "Option 2" text (not the trigger)
		const selectedOption = screen.getAllByText("Option 2")[1].closest("button");
		expect(selectedOption).toHaveClass("bg-(--accent-light)");
	});

	it("shows chevron icon", () => {
		render(<FilterDropdown options={options} value="" onChange={onChange} />);
		// aria-label is default placeholder "Filter"
		const triggerButton = screen.getByRole("button", { name: "Filter" });
		// ChevronDown icon should be present
		expect(triggerButton.querySelector("svg")).toBeInTheDocument();
	});
});
