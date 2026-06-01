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
});
