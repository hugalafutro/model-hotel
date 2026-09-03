import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "../../test/utils";
import { SettingToggleRow } from "../SettingToggleRow";

describe("SettingToggleRow", () => {
	it("names the switch after the label and flips it on click", async () => {
		const onChange = vi.fn();
		renderWithProviders(
			<SettingToggleRow
				label="Row label"
				description="Row description"
				checked={false}
				onChange={onChange}
			/>,
		);

		expect(screen.getByText("Row description")).toBeInTheDocument();
		const toggle = screen.getByRole("switch", { name: "Row label" });
		expect(toggle).toHaveAttribute("aria-checked", "false");
		await userEvent.click(toggle);
		expect(onChange).toHaveBeenCalledWith(true);
		// No reset was offered, so the switch is the row's only control.
		expect(screen.queryByRole("button")).toBeNull();
	});

	it("renders the reset beside the label only when one is given", async () => {
		const onReset = vi.fn();
		renderWithProviders(
			<SettingToggleRow
				testId="the-row"
				label="Row label"
				description="Row description"
				checked={true}
				onChange={() => {}}
				onReset={onReset}
			/>,
		);

		const row = screen.getByTestId("the-row");
		expect(within(row).getByRole("switch")).toBeInTheDocument();
		await userEvent.click(within(row).getByRole("button"));
		expect(onReset).toHaveBeenCalledTimes(1);
	});

	it("locks the switch and the reset independently", () => {
		renderWithProviders(
			<SettingToggleRow
				className="extra-class"
				label="Row label"
				description="Row description"
				checked={true}
				onChange={() => {}}
				disabled
				onReset={() => {}}
				resetDisabled
			/>,
		);

		const toggle = screen.getByRole("switch", { name: "Row label" });
		expect(toggle).toBeDisabled();
		expect(screen.getByRole("button")).toBeDisabled();
		expect(toggle.parentElement).toHaveClass("extra-class");
	});
});
