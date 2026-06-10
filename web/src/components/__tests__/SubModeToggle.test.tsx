import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { LucideProps } from "lucide-react";
import { describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "../../test/utils";
import { SubModeToggle } from "../SubModeToggle";

const StubIcon = Object.assign(
	({ className }: LucideProps) => (
		<svg className={className} data-testid="stub-icon" />
	),
	{ $$typeof: Symbol.for("react.forward_ref") },
);

const listOpt = { value: "list", label: "List", icon: StubIcon };
const gridOpt = { value: "grid", label: "Grid", icon: StubIcon };
const options = [listOpt, gridOpt] as [typeof listOpt, typeof gridOpt];

describe("SubModeToggle", () => {
	it("renders options", () => {
		const onChange = vi.fn();
		renderWithProviders(
			<SubModeToggle options={options} value="list" onChange={onChange} />,
		);

		expect(screen.getByText("List")).toBeInTheDocument();
		expect(screen.getByText("Grid")).toBeInTheDocument();
	});

	it("highlights active option", () => {
		const onChange = vi.fn();
		renderWithProviders(
			<SubModeToggle options={options} value="list" onChange={onChange} />,
		);

		const listButton = screen.getByText("List").closest("button");
		const gridButton = screen.getByText("Grid").closest("button");

		expect(listButton).toHaveClass("bg-(--accent)/20");
		expect(listButton).toHaveClass("text-(--accent)");
		expect(listButton).toHaveClass("border-(--accent)/40");
		expect(listButton).toHaveClass("cursor-default");

		expect(gridButton).not.toHaveClass("bg-(--accent)/20");
		expect(gridButton).not.toHaveClass("text-(--accent)");
	});

	it("calls onChange with selected value", async () => {
		const onChange = vi.fn();
		const user = userEvent.setup();
		renderWithProviders(
			<SubModeToggle options={options} value="list" onChange={onChange} />,
		);

		const gridButton = screen.getByText("Grid").closest("button");
		await user.click(gridButton as HTMLElement);

		expect(onChange).toHaveBeenCalledTimes(1);
		expect(onChange).toHaveBeenCalledWith("grid");
	});

	it("calls onChange when clicking the other option", async () => {
		const onChange = vi.fn();
		const user = userEvent.setup();
		renderWithProviders(
			<SubModeToggle options={options} value="grid" onChange={onChange} />,
		);

		const listButton = screen.getByText("List").closest("button");
		await user.click(listButton as HTMLElement);

		expect(onChange).toHaveBeenCalledTimes(1);
		expect(onChange).toHaveBeenCalledWith("list");
	});

	it("renders disabled state with correct styling", () => {
		const onChange = vi.fn();
		renderWithProviders(
			<SubModeToggle
				options={options}
				value="list"
				onChange={onChange}
				disabled
			/>,
		);

		const listButton = screen.getByText("List").closest("button");
		const gridButton = screen.getByText("Grid").closest("button");

		expect(listButton).toHaveClass("cursor-default");
		expect(gridButton).toHaveClass("cursor-default");
		expect(gridButton).toHaveClass("text-(--text-tertiary)");
	});

	it("renders icons", () => {
		const onChange = vi.fn();
		renderWithProviders(
			<SubModeToggle options={options} value="list" onChange={onChange} />,
		);

		const icons = screen.getAllByTestId("stub-icon");
		expect(icons).toHaveLength(2);
	});

	it("active option has cursor-default", () => {
		const onChange = vi.fn();
		renderWithProviders(
			<SubModeToggle options={options} value="list" onChange={onChange} />,
		);

		const listButton = screen.getByText("List").closest("button");
		expect(listButton).toHaveClass("cursor-default");
	});

	it("inactive option has cursor-pointer when not disabled", () => {
		const onChange = vi.fn();
		renderWithProviders(
			<SubModeToggle options={options} value="list" onChange={onChange} />,
		);

		const gridButton = screen.getByText("Grid").closest("button");
		expect(gridButton).toHaveClass("cursor-pointer");
	});
});
