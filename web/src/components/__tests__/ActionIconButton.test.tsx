import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { Settings } from "@/lib/icons";
import { ActionIconButton } from "../ActionIconButton";

describe("ActionIconButton", () => {
	const onClick = vi.fn();

	beforeEach(() => {
		onClick.mockClear();
	});

	it("renders icon button with title", () => {
		render(
			<ActionIconButton
				icon={Settings}
				onClick={onClick}
				title="Settings"
				color="amber"
			/>,
		);
		const button = screen.getByRole("button");
		expect(button).toHaveAttribute("title", "Settings");
	});

	it("calls onClick when clicked", async () => {
		const user = userEvent.setup();
		render(
			<ActionIconButton
				icon={Settings}
				onClick={onClick}
				title="Settings"
				color="amber"
			/>,
		);
		await user.click(screen.getByRole("button"));
		expect(onClick).toHaveBeenCalledTimes(1);
	});

	it("applies pulse class when pulse=true", () => {
		render(
			<ActionIconButton
				icon={Settings}
				onClick={onClick}
				title="Settings"
				color="amber"
				pulse
			/>,
		);
		expect(screen.getByRole("button")).toHaveClass(
			"animate-[pulse-ring_1.5s_ease-in-out_infinite]",
		);
	});

	it("renders with label when withLabel=true", () => {
		render(
			<ActionIconButton
				icon={Settings}
				onClick={onClick}
				title="Settings"
				color="amber"
				label="Configure"
				withLabel
			/>,
		);
		const button = screen.getByRole("button");
		expect(button).toHaveTextContent("Configure");
		expect(button).toHaveClass("ui-btn");
	});

	it("applies amber color classes", () => {
		render(
			<ActionIconButton
				icon={Settings}
				onClick={onClick}
				title="Settings"
				color="amber"
			/>,
		);
		expect(screen.getByRole("button")).toHaveClass("ui-icon-btn-warning");
	});

	it("applies red color classes", () => {
		render(
			<ActionIconButton
				icon={Settings}
				onClick={onClick}
				title="Delete"
				color="red"
			/>,
		);
		expect(screen.getByRole("button")).toHaveClass("ui-icon-btn-danger");
	});

	it("uses default size 14", () => {
		render(
			<ActionIconButton
				icon={Settings}
				onClick={onClick}
				title="Settings"
				color="amber"
			/>,
		);
		const icon = screen.getByRole("button").querySelector("svg");
		expect(icon).toHaveAttribute("width", "14");
	});

	it("uses custom size", () => {
		render(
			<ActionIconButton
				icon={Settings}
				onClick={onClick}
				title="Settings"
				color="amber"
				size={20}
			/>,
		);
		const icon = screen.getByRole("button").querySelector("svg");
		expect(icon).toHaveAttribute("width", "20");
	});
});
