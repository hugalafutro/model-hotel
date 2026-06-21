import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ParamSlider } from "../ParamSlider";

describe("ParamSlider", () => {
	const onChange = vi.fn();

	beforeEach(() => {
		onChange.mockClear();
	});

	it("renders label", () => {
		render(
			<ParamSlider
				label="Temperature"
				value={0.5}
				min={0}
				max={1}
				step={0.1}
				onChange={onChange}
			/>,
		);
		expect(screen.getByText("Temperature")).toBeInTheDocument();
	});

	it("renders value in number input", () => {
		render(
			<ParamSlider
				label="Temperature"
				value={0.7}
				min={0}
				max={1}
				step={0.1}
				onChange={onChange}
			/>,
		);
		const numberInput = screen.getByRole("spinbutton");
		expect(numberInput).toHaveValue(0.7);
	});

	it("renders slider with correct value", () => {
		render(
			<ParamSlider
				label="Temperature"
				value={0.5}
				min={0}
				max={1}
				step={0.1}
				onChange={onChange}
			/>,
		);
		const slider = screen.getByRole("slider");
		// HTML input values are strings, not numbers
		expect(slider).toHaveValue("0.5");
	});

	it("respects min value", () => {
		render(
			<ParamSlider
				label="Temperature"
				value={0}
				min={0}
				max={1}
				step={0.1}
				onChange={onChange}
			/>,
		);
		const slider = screen.getByRole("slider");
		expect(slider).toHaveAttribute("min", "0");
		expect(slider).toHaveAttribute("max", "1");
		expect(slider).toHaveAttribute("step", "0.1");
	});

	it("respects max value", () => {
		render(
			<ParamSlider
				label="Temperature"
				value={1}
				min={0}
				max={1}
				step={0.1}
				onChange={onChange}
			/>,
		);
		const slider = screen.getByRole("slider");
		expect(slider).toHaveAttribute("max", "1");
	});

	it("is disabled when disabled prop is true", () => {
		render(
			<ParamSlider
				label="Temperature"
				value={0.5}
				min={0}
				max={1}
				step={0.1}
				onChange={onChange}
				disabled
			/>,
		);
		const slider = screen.getByRole("slider");
		const numberInput = screen.getByRole("spinbutton");
		expect(slider).toBeDisabled();
		expect(numberInput).toBeDisabled();
	});

	it("does not call onChange when disabled and slider is clicked", async () => {
		const user = userEvent.setup();
		render(
			<ParamSlider
				label="Temperature"
				value={0.5}
				min={0}
				max={1}
				step={0.1}
				onChange={onChange}
				disabled
			/>,
		);
		const slider = screen.getByRole("slider");
		await user.click(slider);
		expect(onChange).not.toHaveBeenCalled();
	});

	it("shows tooltip on hover when disabled with disabledReason", async () => {
		const user = userEvent.setup();
		render(
			<ParamSlider
				label="Temperature"
				value={0.5}
				min={0}
				max={1}
				step={0.1}
				onChange={onChange}
				disabled
				disabledReason="Feature not available"
			/>,
		);
		const container = screen.getByText("Temperature").parentElement;
		expect(container).not.toBeNull();
		await user.hover(container as HTMLElement);
		expect(screen.getByText("Feature not available")).toBeInTheDocument();
	});

	it("renders placeholder when value is undefined", () => {
		render(
			<ParamSlider
				label="Temperature"
				value={undefined}
				min={0}
				max={1}
				step={0.1}
				onChange={onChange}
			/>,
		);
		const numberInput = screen.getByRole("spinbutton");
		expect(numberInput).toHaveAttribute("placeholder", "off");
		// When value is undefined, the number input has empty string value
		expect(numberInput.getAttribute("value")).toBe("");
	});

	it("calls onChange(undefined) when number input is cleared to empty string", async () => {
		const user = userEvent.setup();
		render(
			<ParamSlider
				label="Temperature"
				value={0.5}
				min={0}
				max={1}
				step={0.1}
				onChange={onChange}
			/>,
		);
		const spinbutton = screen.getByRole("spinbutton");
		await user.clear(spinbutton);
		expect(onChange).toHaveBeenCalledWith(undefined);
	});

	it("calls onChange(undefined) when number input value is just a dash", () => {
		render(
			<ParamSlider
				label="Temperature"
				value={0.5}
				min={0}
				max={1}
				step={0.1}
				onChange={onChange}
			/>,
		);
		const spinbutton = screen.getByRole("spinbutton");
		fireEvent.change(spinbutton, { target: { value: "-" } });
		expect(onChange).toHaveBeenCalledWith(undefined);
	});

	it("calls onChange(undefined) when number input value is just a dot", () => {
		render(
			<ParamSlider
				label="Temperature"
				value={0.5}
				min={0}
				max={1}
				step={0.1}
				onChange={onChange}
			/>,
		);
		const spinbutton = screen.getByRole("spinbutton");
		fireEvent.change(spinbutton, { target: { value: "." } });
		expect(onChange).toHaveBeenCalledWith(undefined);
	});

	it("hides tooltip on mouse leave", async () => {
		const user = userEvent.setup();
		render(
			<ParamSlider
				label="Temperature"
				value={0.5}
				min={0}
				max={1}
				step={0.1}
				onChange={onChange}
				disabled
				disabledReason="Not available"
			/>,
		);
		const container = screen.getByText("Temperature").parentElement;
		expect(container).not.toBeNull();
		await user.hover(container as HTMLElement);
		expect(screen.getByRole("tooltip")).toBeInTheDocument();
		await user.unhover(container as HTMLElement);
		expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
	});

	it("does not show tooltip when disabled but no disabledReason", async () => {
		const user = userEvent.setup();
		render(
			<ParamSlider
				label="Temperature"
				value={0.5}
				min={0}
				max={1}
				step={0.1}
				onChange={onChange}
				disabled
			/>,
		);
		const container = screen.getByText("Temperature").parentElement;
		expect(container).not.toBeNull();
		await user.hover(container as HTMLElement);
		expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
	});

	it("does not call onChange from range slider when disabled", () => {
		render(
			<ParamSlider
				label="Temperature"
				value={0.5}
				min={0}
				max={1}
				step={0.1}
				onChange={onChange}
				disabled
			/>,
		);
		const slider = screen.getByRole("slider");
		fireEvent.change(slider, { target: { value: "0.8" } });
		expect(onChange).not.toHaveBeenCalled();
	});
});
