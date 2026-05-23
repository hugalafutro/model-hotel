import { fireEvent, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithProviders } from "../../../test/utils";
import { ColorPickerModal } from "../ColorPickerModal";

describe("ColorPickerModal", () => {
	it("renders modal with title", () => {
		const onClose = vi.fn();
		const onChange = vi.fn();
		const onApply = vi.fn();

		renderWithProviders(
			<ColorPickerModal
				color="#3b82f6"
				onChange={onChange}
				onClose={onClose}
				onApply={onApply}
			/>,
		);

		expect(screen.getByText("Pick a Color")).toBeInTheDocument();
	});

	it("displays current color value in input", () => {
		const onClose = vi.fn();
		const onChange = vi.fn();
		const onApply = vi.fn();

		renderWithProviders(
			<ColorPickerModal
				color="#3b82f6"
				onChange={onChange}
				onClose={onClose}
				onApply={onApply}
			/>,
		);

		const input = screen.getByRole("textbox") as HTMLInputElement;
		expect(input.value).toBe("3b82f6");
	});

	it("calls onClose when cancel button is clicked", async () => {
		const user = userEvent.setup();
		const onClose = vi.fn();
		const onChange = vi.fn();
		const onApply = vi.fn();

		renderWithProviders(
			<ColorPickerModal
				color="#3b82f6"
				onChange={onChange}
				onClose={onClose}
				onApply={onApply}
			/>,
		);

		const cancelButton = screen.getByRole("button", { name: /Cancel/i });
		await user.click(cancelButton);

		expect(onClose).toHaveBeenCalledTimes(1);
	});

	it("calls onApply when apply button is clicked", async () => {
		const user = userEvent.setup();
		const onClose = vi.fn();
		const onChange = vi.fn();
		const onApply = vi.fn();

		renderWithProviders(
			<ColorPickerModal
				color="#3b82f6"
				onChange={onChange}
				onClose={onClose}
				onApply={onApply}
			/>,
		);

		const applyButton = screen.getByRole("button", { name: /Apply/i });
		await user.click(applyButton);

		expect(onApply).toHaveBeenCalledTimes(1);
	});

	it("calls onChange when typing in hex input", () => {
		const onClose = vi.fn();
		const onChange = vi.fn();
		const onApply = vi.fn();

		renderWithProviders(
			<ColorPickerModal
				color="#3b82f6"
				onChange={onChange}
				onClose={onClose}
				onApply={onApply}
			/>,
		);

		const input = screen.getByRole("textbox") as HTMLInputElement;
		fireEvent.change(input, { target: { value: "ff0000" } });

		expect(onChange).toHaveBeenCalledWith("#ff0000");
	});

	it("strips non-hex characters from hex input", async () => {
		const user = userEvent.setup();
		const onClose = vi.fn();
		const onChange = vi.fn();
		const onApply = vi.fn();

		renderWithProviders(
			<ColorPickerModal
				color="#3b82f6"
				onChange={onChange}
				onClose={onClose}
				onApply={onApply}
			/>,
		);

		const input = screen.getByRole("textbox") as HTMLInputElement;
		await user.clear(input);
		await user.type(input, "gg");

		expect(onChange).toHaveBeenCalledWith("#");
	});

	it("does not call onChange when stripped value exceeds 6 characters", () => {
		const onClose = vi.fn();
		const onChange = vi.fn();
		const onApply = vi.fn();

		renderWithProviders(
			<ColorPickerModal
				color="#3b82f6"
				onChange={onChange}
				onClose={onClose}
				onApply={onApply}
			/>,
		);

		const input = screen.getByRole("textbox") as HTMLInputElement;

		// The component's onChange handler strips non-hex and guards on length:
		// if (val.length <= 6) { onChange(`#${val}`) }
		// So an 8-char hex value (>6 after stripping) should NOT trigger onChange
		fireEvent.change(input, { target: { value: "abcdef12" } });

		expect(input.maxLength).toBe(6);
		expect(onChange).not.toHaveBeenCalled();
	});

	it("renders color preview swatch with correct background color", () => {
		const onClose = vi.fn();
		const onChange = vi.fn();
		const onApply = vi.fn();
		const testColor = "#ff5500";

		renderWithProviders(
			<ColorPickerModal
				color={testColor}
				onChange={onChange}
				onClose={onClose}
				onApply={onApply}
			/>,
		);

		const swatch = screen.getByTestId("color-preview") as HTMLElement;
		expect(swatch).toHaveStyle(`background-color: ${testColor}`);
	});

	it("renders # prefix label", () => {
		const onClose = vi.fn();
		const onChange = vi.fn();
		const onApply = vi.fn();

		renderWithProviders(
			<ColorPickerModal
				color="#3b82f6"
				onChange={onChange}
				onClose={onClose}
				onApply={onApply}
			/>,
		);

		expect(screen.getByText("#")).toBeInTheDocument();
	});

	it("updates hex input value when color prop changes", () => {
		const onClose = vi.fn();
		const onChange = vi.fn();
		const onApply = vi.fn();
		const { rerender } = renderWithProviders(
			<ColorPickerModal
				color="#3b82f6"
				onChange={onChange}
				onClose={onClose}
				onApply={onApply}
			/>,
		);

		const input = screen.getByRole("textbox") as HTMLInputElement;
		expect(input.value).toBe("3b82f6");

		rerender(
			<ColorPickerModal
				color="#ff0000"
				onChange={onChange}
				onClose={onClose}
				onApply={onApply}
			/>,
		);

		expect(input.value).toBe("ff0000");
	});

	it("calls onClose when modal overlay is clicked", async () => {
		const user = userEvent.setup();
		const onClose = vi.fn();
		const onChange = vi.fn();
		const onApply = vi.fn();

		renderWithProviders(
			<ColorPickerModal
				color="#3b82f6"
				onChange={onChange}
				onClose={onClose}
				onApply={onApply}
			/>,
		);

		// The Modal renders a backdrop button with aria-label="Close dialog"
		const backdrop = screen.getByRole("button", { name: "Close dialog" });
		await user.click(backdrop);
		expect(onClose).toHaveBeenCalledTimes(1);
	});
});
