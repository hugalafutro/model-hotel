import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { RevealableInput } from "../RevealableInput";

describe("RevealableInput", () => {
	it("masks the value until the reveal button is pressed", async () => {
		const user = userEvent.setup();
		render(<RevealableInput id="key" value="sk-secret" onChange={() => {}} />);
		const input = screen.getByDisplayValue("sk-secret");
		expect(input).toHaveAttribute("type", "password");

		await user.click(screen.getByRole("button"));
		expect(screen.getByDisplayValue("sk-secret")).toHaveAttribute(
			"type",
			"text",
		);

		await user.click(screen.getByRole("button"));
		expect(screen.getByDisplayValue("sk-secret")).toHaveAttribute(
			"type",
			"password",
		);
	});

	it("reports every keystroke and keeps the reveal button out of the tab order", async () => {
		const user = userEvent.setup();
		const onChange = vi.fn();
		render(
			<RevealableInput
				id="key"
				value=""
				onChange={onChange}
				placeholder="sk-..."
			/>,
		);

		await user.type(screen.getByPlaceholderText("sk-..."), "ab");
		expect(onChange).toHaveBeenCalledTimes(2);
		expect(screen.getByRole("button")).toHaveAttribute("tabindex", "-1");
	});
});
