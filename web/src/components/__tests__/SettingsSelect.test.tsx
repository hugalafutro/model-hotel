import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { SettingsSelect } from "../SettingsSelect";

const options = [
	{ value: "a", label: "Option A" },
	{ value: "b", label: "Option B" },
];

describe("SettingsSelect", () => {
	const onChange = vi.fn();

	beforeEach(() => {
		onChange.mockClear();
	});

	it("renders select with matching value", () => {
		render(
			<SettingsSelect
				id="test-select"
				label="Test Label"
				value="a"
				options={options}
				onChange={onChange}
			/>,
		);
		const select = screen.getByRole("combobox");
		expect(select).toHaveValue("a");
	});

	it("renders input when value is custom (not in options)", () => {
		render(
			<SettingsSelect
				id="test-select"
				label="Test Label"
				value="custom-value"
				options={options}
				onChange={onChange}
			/>,
		);
		const input = screen.getByRole("textbox");
		expect(input).toHaveValue("custom-value");
	});

	it("calls onChange when select value changes", async () => {
		const user = userEvent.setup();
		render(
			<SettingsSelect
				id="test-select"
				label="Test Label"
				value="a"
				options={options}
				onChange={onChange}
			/>,
		);
		await user.selectOptions(screen.getByRole("combobox"), "b");
		expect(onChange).toHaveBeenCalledWith("b");
	});

	it("calls onChange when input value changes", async () => {
		const user = userEvent.setup();
		render(
			<SettingsSelect
				id="test-select"
				label="Test Label"
				value="custom"
				options={options}
				onChange={onChange}
			/>,
		);
		const input = screen.getByRole("textbox");
		await user.type(input, "x");
		expect(onChange).toHaveBeenCalledWith("customx");
	});

	it("renders label", () => {
		render(
			<SettingsSelect
				id="test-select"
				label="Test Label"
				value="a"
				options={options}
				onChange={onChange}
			/>,
		);
		expect(screen.getByLabelText("Test Label")).toBeInTheDocument();
	});

	it("renders description when provided", () => {
		render(
			<SettingsSelect
				id="test-select"
				label="Test Label"
				value="a"
				options={options}
				onChange={onChange}
				description="This is a description"
			/>,
		);
		expect(screen.getByText("This is a description")).toBeInTheDocument();
	});

	it("does not render description when not provided", () => {
		render(
			<SettingsSelect
				id="test-select"
				label="Test Label"
				value="a"
				options={options}
				onChange={onChange}
			/>,
		);
		expect(screen.queryByText(/description/i)).not.toBeInTheDocument();
	});

	it("disables select when disabled=true", () => {
		render(
			<SettingsSelect
				id="test-select"
				label="Test Label"
				value="a"
				options={options}
				onChange={onChange}
				disabled
			/>,
		);
		expect(screen.getByRole("combobox")).toBeDisabled();
	});

	it("disables input when disabled=true with custom value", () => {
		render(
			<SettingsSelect
				id="test-select"
				label="Test Label"
				value="custom"
				options={options}
				onChange={onChange}
				disabled
			/>,
		);
		expect(screen.getByRole("textbox")).toBeDisabled();
	});

	it("renders all options", () => {
		render(
			<SettingsSelect
				id="test-select"
				label="Test Label"
				value="a"
				options={options}
				onChange={onChange}
			/>,
		);
		const select = screen.getByRole("combobox");
		expect(select).toContainHTML('<option value="a">Option A</option>');
		expect(select).toContainHTML('<option value="b">Option B</option>');
	});
});
