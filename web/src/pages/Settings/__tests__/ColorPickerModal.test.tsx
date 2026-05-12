import { screen } from "@testing-library/react";
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
});
