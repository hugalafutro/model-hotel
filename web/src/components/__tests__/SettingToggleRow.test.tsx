import { screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "../../test/utils";
import { SettingToggleRow } from "../SettingToggleRow";

describe("SettingToggleRow", () => {
	it("names the switch after the label and flips it on click", async () => {
		const onChange = vi.fn();
		const { user } = renderWithProviders(
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
		await user.click(toggle);
		expect(onChange).toHaveBeenCalledWith(true);
		// No reset was offered, so the switch is the row's only control.
		expect(screen.queryByRole("button")).toBeNull();
	});

	it("renders the reset beside the label only when one is given", async () => {
		const onChange = vi.fn();
		const onReset = vi.fn();
		const { user } = renderWithProviders(
			<SettingToggleRow
				testId="the-row"
				label="Row label"
				description="Row description"
				checked={true}
				onChange={onChange}
				onReset={onReset}
			/>,
		);

		const row = screen.getByTestId("the-row");
		const toggle = within(row).getByRole("switch");
		expect(toggle).toHaveAttribute("aria-checked", "true");
		await user.click(toggle);
		expect(onChange).toHaveBeenCalledWith(false);
		await user.click(within(row).getByRole("button"));
		expect(onReset).toHaveBeenCalledTimes(1);
	});

	it("locks the switch and the reset independently", () => {
		renderWithProviders(
			<SettingToggleRow
				testId="the-row"
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

		expect(screen.getByRole("switch", { name: "Row label" })).toBeDisabled();
		expect(screen.getByRole("button")).toBeDisabled();
		expect(screen.getByTestId("the-row")).toHaveClass("extra-class");
	});
});
