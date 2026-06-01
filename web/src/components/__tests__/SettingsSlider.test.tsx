import { fireEvent, screen } from "@testing-library/react";
import { renderWithProviders } from "../../test/utils";
import { SettingsSlider } from "../SettingsSlider";

describe("SettingsSlider", () => {
	const defaultProps = {
		id: "test-slider",
		label: "Temperature",
		value: 50,
		min: 0,
		max: 100,
		step: 1,
		onChange: vi.fn(),
	};

	beforeEach(() => {
		vi.clearAllMocks();
	});

	it("renders label and current value", () => {
		renderWithProviders(<SettingsSlider {...defaultProps} />);
		expect(screen.getByText("Temperature")).toBeInTheDocument();
		expect(screen.getAllByDisplayValue("50")).toHaveLength(2);
	});

	it("fires onChange with slider value", () => {
		const onChange = vi.fn();
		renderWithProviders(
			<SettingsSlider {...defaultProps} onChange={onChange} />,
		);
		const slider = screen.getByRole("slider");
		fireEvent.change(slider, { target: { value: 75 } });
		expect(onChange).toHaveBeenCalledWith(75);
	});

	it("fires onChange with clamped value from number input", () => {
		const onChange = vi.fn();
		renderWithProviders(
			<SettingsSlider {...defaultProps} onChange={onChange} />,
		);
		const numberInput = screen.getByRole("spinbutton");
		fireEvent.change(numberInput, { target: { value: 150 } });
		expect(onChange).toHaveBeenCalledWith(100);
	});

	it("clamps number input value to min", () => {
		const onChange = vi.fn();
		renderWithProviders(
			<SettingsSlider {...defaultProps} onChange={onChange} />,
		);
		const numberInput = screen.getByRole("spinbutton");
		fireEvent.change(numberInput, { target: { value: -10 } });
		expect(onChange).toHaveBeenCalledWith(0);
	});

	it("snaps value to clampStep from slider", () => {
		const onChange = vi.fn();
		renderWithProviders(
			<SettingsSlider {...defaultProps} clampStep={5} onChange={onChange} />,
		);
		const slider = screen.getByRole("slider");
		fireEvent.change(slider, { target: { value: 12 } });
		expect(onChange).toHaveBeenCalledWith(10);
	});

	it("snaps value to clampStep from number input", () => {
		const onChange = vi.fn();
		renderWithProviders(
			<SettingsSlider {...defaultProps} clampStep={5} onChange={onChange} />,
		);
		const numberInput = screen.getByRole("spinbutton");
		fireEvent.change(numberInput, { target: { value: 33 } });
		expect(onChange).toHaveBeenCalledWith(35);
	});

	it("renders description when provided", () => {
		renderWithProviders(
			<SettingsSlider {...defaultProps} description="Controls randomness" />,
		);
		expect(screen.getByText("Controls randomness")).toBeInTheDocument();
	});

	it("does not render description when not provided", () => {
		renderWithProviders(<SettingsSlider {...defaultProps} />);
		expect(screen.queryByText("Controls randomness")).not.toBeInTheDocument();
	});

	it("applies disabled styling when disabled", () => {
		renderWithProviders(<SettingsSlider {...defaultProps} disabled={true} />);
		const container = screen.getByText("Temperature").closest("div.opacity-50");
		expect(container).toBeInTheDocument();
	});

	it("renders unit suffix when provided", () => {
		renderWithProviders(<SettingsSlider {...defaultProps} unit="ms" />);
		expect(screen.getByText("ms")).toBeInTheDocument();
	});

	it("does not render unit suffix when not provided", () => {
		renderWithProviders(<SettingsSlider {...defaultProps} />);
		expect(screen.queryByText("ms")).not.toBeInTheDocument();
	});

	it("renders hidden unit when hideUnit is true", () => {
		renderWithProviders(<SettingsSlider {...defaultProps} unit="s" hideUnit />);
		const unitSpan = screen.getByText("s");
		expect(unitSpan).toHaveClass("text-transparent");
		expect(unitSpan).toHaveAttribute("aria-hidden", "true");
	});

	it("step up button increments value", () => {
		const onChange = vi.fn();
		renderWithProviders(
			<SettingsSlider
				{...defaultProps}
				value={50}
				step={5}
				onChange={onChange}
			/>,
		);
		const buttons = screen.getAllByRole("button");
		const stepUpBtn = buttons[0];
		fireEvent.click(stepUpBtn);
		expect(onChange).toHaveBeenCalledWith(55);
	});

	it("step down button decrements value", () => {
		const onChange = vi.fn();
		renderWithProviders(
			<SettingsSlider
				{...defaultProps}
				value={50}
				step={5}
				onChange={onChange}
			/>,
		);
		const buttons = screen.getAllByRole("button");
		const stepDownBtn = buttons[1];
		fireEvent.click(stepDownBtn);
		expect(onChange).toHaveBeenCalledWith(45);
	});

	it("step up is disabled at max", () => {
		renderWithProviders(
			<SettingsSlider {...defaultProps} value={100} max={100} />,
		);
		const buttons = screen.getAllByRole("button");
		expect(buttons[0]).toBeDisabled();
	});

	it("step down is disabled at min", () => {
		renderWithProviders(<SettingsSlider {...defaultProps} value={0} min={0} />);
		const buttons = screen.getAllByRole("button");
		expect(buttons[1]).toBeDisabled();
	});
	describe("infinityValue", () => {
		it("displays ∞ when value reaches infinityValue", () => {
			renderWithProviders(
				<SettingsSlider {...defaultProps} value={0} infinityValue={0} />,
			);
			const numberInput = screen.getByRole("spinbutton");
			expect(numberInput).toHaveAttribute("value", "∞");
		});

		it("stepUp escapes from infinity to first valid step", () => {
			const onChange = vi.fn();
			renderWithProviders(
				<SettingsSlider
					{...defaultProps}
					value={0}
					min={0}
					max={100}
					step={5}
					infinityValue={0}
					onChange={onChange}
				/>,
			);
			const buttons = screen.getAllByRole("button");
			const stepUpBtn = buttons[0];
			fireEvent.click(stepUpBtn);
			expect(onChange).toHaveBeenCalledWith(5);
		});

		it("stepUp escapes from infinity using min when min > infinityValue", () => {
			const onChange = vi.fn();
			renderWithProviders(
				<SettingsSlider
					{...defaultProps}
					value={0}
					min={5}
					max={100}
					step={5}
					infinityValue={0}
					onChange={onChange}
				/>,
			);
			const buttons = screen.getAllByRole("button");
			const stepUpBtn = buttons[0];
			fireEvent.click(stepUpBtn);
			expect(onChange).toHaveBeenCalledWith(5);
		});

		it("number input is readOnly when infinity", () => {
			renderWithProviders(
				<SettingsSlider {...defaultProps} value={0} infinityValue={0} />,
			);
			const numberInput = screen.getByRole("spinbutton");
			expect(numberInput).toHaveAttribute("readOnly");
		});

		it("stepDown is disabled when value equals min and isInfinity", () => {
			renderWithProviders(
				<SettingsSlider
					{...defaultProps}
					value={0}
					min={0}
					infinityValue={0}
				/>,
			);
			const buttons = screen.getAllByRole("button");
			const stepDownBtn = buttons[1];
			expect(stepDownBtn).toBeDisabled();
		});
	});

	it("clamps to step on blur when value is not aligned to step", () => {
		const onChange = vi.fn();
		renderWithProviders(
			<SettingsSlider
				{...defaultProps}
				value={53}
				step={5}
				clampStep={5}
				onChange={onChange}
			/>,
		);
		const numberInput = screen.getByRole("spinbutton");
		fireEvent.blur(numberInput);
		expect(onChange).toHaveBeenCalledWith(55);
	});

	it("does not fire onChange on blur when value is already clamped to step", () => {
		const onChange = vi.fn();
		renderWithProviders(
			<SettingsSlider
				{...defaultProps}
				value={55}
				step={5}
				clampStep={5}
				onChange={onChange}
			/>,
		);
		const numberInput = screen.getByRole("spinbutton");
		fireEvent.blur(numberInput);
		expect(onChange).not.toHaveBeenCalled();
	});
});
