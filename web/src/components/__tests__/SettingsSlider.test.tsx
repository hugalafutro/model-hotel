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

	it("does NOT fire onChange during slider drag", () => {
		const onChange = vi.fn();
		renderWithProviders(
			<SettingsSlider {...defaultProps} onChange={onChange} />,
		);
		const slider = screen.getByRole("slider");
		fireEvent.change(slider, { target: { value: 75 } });
		expect(onChange).not.toHaveBeenCalled();
	});

	it("fires onChange on pointerUp after slider drag", () => {
		const onChange = vi.fn();
		renderWithProviders(
			<SettingsSlider {...defaultProps} onChange={onChange} />,
		);
		const slider = screen.getByRole("slider");
		fireEvent.change(slider, { target: { value: 75 } });
		fireEvent.pointerUp(slider);
		expect(onChange).toHaveBeenCalledWith(75);
	});

	it("does NOT fire onChange on keyUp when arrow key doesn't change value", () => {
		const onChange = vi.fn();
		renderWithProviders(
			<SettingsSlider {...defaultProps} onChange={onChange} />,
		);
		const slider = screen.getByRole("slider");
		fireEvent.keyUp(slider, { key: "ArrowRight" });
		expect(onChange).not.toHaveBeenCalled();
	});

	it("fires onChange on keyUp when arrow key changes value", () => {
		const onChange = vi.fn();
		renderWithProviders(
			<SettingsSlider {...defaultProps} value={0} onChange={onChange} />,
		);
		const slider = screen.getByRole("slider");
		fireEvent.change(slider, { target: { value: 1 } });
		fireEvent.keyUp(slider, { key: "ArrowRight" });
		expect(onChange).toHaveBeenCalledWith(1);
	});

	it("does NOT fire onChange on keyUp for non-navigation keys", () => {
		const onChange = vi.fn();
		renderWithProviders(
			<SettingsSlider {...defaultProps} onChange={onChange} />,
		);
		const slider = screen.getByRole("slider");
		fireEvent.keyUp(slider, { key: "a" });
		expect(onChange).not.toHaveBeenCalled();
	});

	it("does NOT fire onChange during number input typing", () => {
		const onChange = vi.fn();
		renderWithProviders(
			<SettingsSlider {...defaultProps} onChange={onChange} />,
		);
		const numberInput = screen.getByRole("spinbutton");
		fireEvent.change(numberInput, { target: { value: 80 } });
		expect(onChange).not.toHaveBeenCalled();
	});

	it("fires onChange on number input blur", () => {
		const onChange = vi.fn();
		renderWithProviders(
			<SettingsSlider {...defaultProps} onChange={onChange} />,
		);
		const numberInput = screen.getByRole("spinbutton");
		fireEvent.change(numberInput, { target: { value: 80 } });
		fireEvent.blur(numberInput);
		expect(onChange).toHaveBeenCalledWith(80);
	});

	it("clamps number input value to max on blur", () => {
		const onChange = vi.fn();
		renderWithProviders(
			<SettingsSlider {...defaultProps} onChange={onChange} />,
		);
		const numberInput = screen.getByRole("spinbutton");
		fireEvent.change(numberInput, { target: { value: 150 } });
		fireEvent.blur(numberInput);
		expect(onChange).toHaveBeenCalledWith(100);
	});

	it("clamps number input value to min on blur", () => {
		const onChange = vi.fn();
		renderWithProviders(
			<SettingsSlider {...defaultProps} onChange={onChange} />,
		);
		const numberInput = screen.getByRole("spinbutton");
		fireEvent.change(numberInput, { target: { value: -10 } });
		fireEvent.blur(numberInput);
		expect(onChange).toHaveBeenCalledWith(0);
	});

	it("snaps value to clampStep from slider on pointerUp", () => {
		const onChange = vi.fn();
		renderWithProviders(
			<SettingsSlider {...defaultProps} clampStep={5} onChange={onChange} />,
		);
		const slider = screen.getByRole("slider");
		fireEvent.change(slider, { target: { value: 12 } });
		fireEvent.pointerUp(slider);
		expect(onChange).toHaveBeenCalledWith(10);
	});

	it("snaps value to clampStep from number input on blur", () => {
		const onChange = vi.fn();
		renderWithProviders(
			<SettingsSlider {...defaultProps} clampStep={5} onChange={onChange} />,
		);
		const numberInput = screen.getByRole("spinbutton");
		fireEvent.change(numberInput, { target: { value: 33 } });
		fireEvent.blur(numberInput);
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

	it("step up button increments value immediately", () => {
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

	it("step down button decrements value immediately", () => {
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
		it("displays ∞ when value equals infinityValue", () => {
			renderWithProviders(
				<SettingsSlider {...defaultProps} value={0} infinityValue={0} />,
			);
			expect(screen.getByText("∞")).toBeInTheDocument();
		});

		it("does NOT display ∞ when value does not equal infinityValue", () => {
			renderWithProviders(
				<SettingsSlider {...defaultProps} value={5} infinityValue={0} />,
			);
			expect(screen.queryByText("∞")).not.toBeInTheDocument();
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

		it("replaces number input with span when infinity", () => {
			renderWithProviders(
				<SettingsSlider {...defaultProps} value={0} infinityValue={0} />,
			);
			const infinitySpan = screen.getByText("∞");
			expect(infinitySpan).toBeInTheDocument();
			expect(screen.queryByRole("spinbutton")).not.toBeInTheDocument();
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

		it("draws slider bar without accent color when at infinityValue", () => {
			renderWithProviders(
				<SettingsSlider {...defaultProps} value={0} infinityValue={0} />,
			);
			const slider = screen.getByRole("slider");
			// When at infinityValue (off/disabled position), the bar should
			// NOT be filled with accent color. pct=0 means no gradient fill.
			expect(slider.style.background).toBe(
				"linear-gradient(to right, var(--accent) 0%, var(--surface-hover) 0%)",
			);
		});

		it("draws slider bar with accent color when NOT at infinityValue", () => {
			renderWithProviders(
				<SettingsSlider
					{...defaultProps}
					value={50}
					min={0}
					max={100}
					infinityValue={0}
				/>,
			);
			const slider = screen.getByRole("slider");
			// When value is 50 of 100, pct should be 50
			expect(slider.style.background).toBe(
				"linear-gradient(to right, var(--accent) 50%, var(--surface-hover) 50%)",
			);
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

	it("does NOT fire onChange on blur when value has not changed", () => {
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
